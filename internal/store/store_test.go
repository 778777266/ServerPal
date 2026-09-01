package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEmptyCollectionsAreNonNil(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "serverpal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	projects, err := database.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := database.Domains(ctx)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := database.AuditLogs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if projects == nil || domains == nil || logs == nil {
		t.Fatalf("empty collections must encode as arrays: projects=%v domains=%v logs=%v", projects, domains, logs)
	}
}

func TestStoreLifecycleAndCascade(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "serverpal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.CreateUser(ctx, "user-1", "admin", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateSession(ctx, Session{ID: "session-1", UserID: "user-1", TokenHash: "token-hash", CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	project := Project{ID: "project-1", Name: "API", RootPath: "/srv/serverpal/apps/api", Runtime: "npm", NPMScript: "start", NodeBinary: "/usr/bin/node", Port: 3000, Instances: 1, PM2Name: "api", Env: map[string]string{"NODE_ENV": "production"}}
	if err := database.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateDomain(ctx, Domain{ID: "domain-1", ProjectID: project.ID, Hostname: "api.example.com", HTTPS: true, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteProject(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Domain(ctx, "domain-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected domain cascade deletion, got %v", err)
	}
}

func TestPasswordUpdateRevokesSessions(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "serverpal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.CreateUser(ctx, "user-1", "admin", "old"); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateSession(ctx, Session{ID: "session-1", UserID: "user-1", TokenHash: "token", CSRFToken: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdatePassword(ctx, "admin", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SessionByTokenHash(ctx, "token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected session revocation, got %v", err)
	}
}

func TestProjectPortsAreUnique(t *testing.T) {
	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "serverpal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	first := Project{ID: "project-1", Name: "API", RootPath: "/srv/serverpal/apps/api", Runtime: "npm", NPMScript: "start", NodeBinary: "/opt/serverpal/node/current/bin/node", Port: 3000, Instances: 1, PM2Name: "api", Env: map[string]string{}}
	second := Project{ID: "project-2", Name: "Worker", RootPath: "/srv/serverpal/apps/worker", Runtime: "npm", NPMScript: "start", NodeBinary: "/opt/serverpal/node/current/bin/node", Port: 3000, Instances: 1, PM2Name: "worker", Env: map[string]string{}}
	if err := database.CreateProject(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateProject(ctx, second); err == nil {
		t.Fatal("duplicate project port was accepted")
	}
}
