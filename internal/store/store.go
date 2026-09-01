package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure sqlite: %w", err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	migrations := []string{`
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  csrf_token TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL,
  runtime TEXT NOT NULL,
  entry TEXT NOT NULL DEFAULT '',
  npm_script TEXT NOT NULL DEFAULT '',
  node_binary TEXT NOT NULL,
  port INTEGER NOT NULL,
  instances INTEGER NOT NULL DEFAULT 1,
  pm2_name TEXT NOT NULL UNIQUE,
  env_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS domains (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  hostname TEXT NOT NULL UNIQUE,
  https INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'pending',
  expires_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_domains_project_id ON domains(project_id);
CREATE TABLE IF NOT EXISTS audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id TEXT NOT NULL,
  username TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
		`, `
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_port ON projects(port);
		`}
	for index, migration := range migrations {
		version := index + 1
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, version, time.Now().Unix()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	_, _ = s.db.ExecContext(ctx, "PRAGMA optimize")
	return nil
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (s *Store) CreateUser(ctx context.Context, id, username, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, created_at) VALUES(?,?,?,?)`,
		id, username, passwordHash, time.Now().Unix())
	return err
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = time.Unix(created, 0)
	return u, err
}

func (s *Store) UpdatePassword(ctx context.Context, username, passwordHash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE username=?`, passwordHash, username)
	if err != nil {
		return err
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?)`, username)
	return err
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, session.CSRFToken, session.ExpiresAt.Unix(), time.Now().Unix())
	return err
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.user_id,u.username,s.token_hash,s.csrf_token,s.expires_at
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash, time.Now().Unix()).
		Scan(&session.ID, &session.UserID, &session.Username, &session.TokenHash, &session.CSRFToken, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	session.ExpiresAt = time.Unix(expires, 0)
	return session, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix())
	return err
}

func (s *Store) CreateProject(ctx context.Context, p Project) error {
	env, err := json.Marshal(p.Env)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,runtime,entry,npm_script,node_binary,port,instances,pm2_name,env_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.RootPath, p.Runtime, p.Entry, p.NPMScript, p.NodeBinary,
		p.Port, p.Instances, p.PM2Name, string(env), now, now)
	return err
}

func (s *Store) UpdateProject(ctx context.Context, p Project) error {
	env, err := json.Marshal(p.Env)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?,root_path=?,runtime=?,entry=?,npm_script=?,node_binary=?,port=?,instances=?,pm2_name=?,env_json=?,updated_at=? WHERE id=?`,
		p.Name, p.RootPath, p.Runtime, p.Entry, p.NPMScript, p.NodeBinary, p.Port, p.Instances, p.PM2Name, string(env), time.Now().Unix(), p.ID)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) Project(ctx context.Context, id string) (Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,root_path,runtime,entry,npm_script,node_binary,port,instances,pm2_name,env_json,created_at,updated_at FROM projects WHERE id=?`, id)
	return scanProject(row)
}

func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,root_path,runtime,entry,npm_script,node_binary,port,instances,pm2_name,env_json,created_at,updated_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanProject(row scanner) (Project, error) {
	var p Project
	var env string
	var created, updated int64
	err := row.Scan(&p.ID, &p.Name, &p.RootPath, &p.Runtime, &p.Entry, &p.NPMScript, &p.NodeBinary, &p.Port, &p.Instances, &p.PM2Name, &env, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if err := json.Unmarshal([]byte(env), &p.Env); err != nil {
		return Project{}, err
	}
	p.CreatedAt = time.Unix(created, 0)
	p.UpdatedAt = time.Unix(updated, 0)
	return p, nil
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM projects WHERE id=?", id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) CreateDomain(ctx context.Context, d Domain) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO domains(id,project_id,hostname,https,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Hostname, boolInt(d.HTTPS), d.Status, now, now)
	return err
}

func (s *Store) Domain(ctx context.Context, id string) (Domain, error) {
	row := s.db.QueryRowContext(ctx, `SELECT d.id,d.project_id,p.name,d.hostname,d.https,d.status,d.expires_at,d.created_at,d.updated_at
		FROM domains d JOIN projects p ON p.id=d.project_id WHERE d.id=?`, id)
	return scanDomain(row)
}

func (s *Store) Domains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.project_id,p.name,d.hostname,d.https,d.status,d.expires_at,d.created_at,d.updated_at
		FROM domains d JOIN projects p ON p.id=d.project_id ORDER BY d.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	domains := make([]Domain, 0)
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

func scanDomain(row scanner) (Domain, error) {
	var d Domain
	var https int
	var expires sql.NullInt64
	var created, updated int64
	err := row.Scan(&d.ID, &d.ProjectID, &d.ProjectName, &d.Hostname, &https, &d.Status, &expires, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, err
	}
	d.HTTPS = https == 1
	if expires.Valid {
		t := time.Unix(expires.Int64, 0)
		d.ExpiresAt = &t
	}
	d.CreatedAt = time.Unix(created, 0)
	d.UpdatedAt = time.Unix(updated, 0)
	return d, nil
}

func (s *Store) UpdateDomainStatus(ctx context.Context, id, status string, expires *time.Time) error {
	var expiresUnix any
	if expires != nil {
		expiresUnix = expires.Unix()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE domains SET status=?,expires_at=?,updated_at=? WHERE id=?`, status, expiresUnix, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM domains WHERE id=?", id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *Store) AddAudit(ctx context.Context, log AuditLog) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs(user_id,username,action,target,detail,ip,created_at) VALUES(?,?,?,?,?,?,?)`,
		log.UserID, log.Username, log.Action, log.Target, log.Detail, log.IP, time.Now().Unix())
	return err
}

func (s *Store) AuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,username,action,target,detail,ip,created_at FROM audit_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]AuditLog, 0)
	for rows.Next() {
		var item AuditLog
		var created int64
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Action, &item.Target, &item.Detail, &item.IP, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = time.Unix(created, 0)
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
