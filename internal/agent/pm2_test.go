package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/serverpal/serverpal/internal/config"
)

func TestSafeLogPathStaysInsidePM2Home(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	if err := os.Mkdir(logs, 0o750); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(logs, "app.log")
	if err := os.WriteFile(managed, []byte("ok"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager := &PM2Manager{cfg: config.Config{PM2Home: root}}
	expected, err := filepath.EvalSymlinks(managed)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := manager.safeLogPath(managed); err != nil || got != expected {
		t.Fatalf("managed log rejected: path=%q err=%v", got, err)
	}

	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.safeLogPath(outside); err == nil {
		t.Fatal("outside log path was accepted")
	}

	symlink := filepath.Join(logs, "escape.log")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.safeLogPath(symlink); err == nil {
		t.Fatal("escaping symlink was accepted")
	}
}
