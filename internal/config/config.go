package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type Config struct {
	Listen          string   `json:"listen"`
	DataDir         string   `json:"dataDir"`
	AgentSocket     string   `json:"agentSocket"`
	AgentGroup      string   `json:"agentGroup"`
	AgentTokenFile  string   `json:"agentTokenFile"`
	AllowedRoots    []string `json:"allowedRoots"`
	ProjectUser     string   `json:"projectUser"`
	ProjectHome     string   `json:"projectHome"`
	PM2Binary       string   `json:"pm2Binary"`
	PM2Home         string   `json:"pm2Home"`
	NodeBinary      string   `json:"nodeBinary"`
	CaddyBinary     string   `json:"caddyBinary"`
	CaddyConfig     string   `json:"caddyConfig"`
	CaddySitesDir   string   `json:"caddySitesDir"`
	CaddyGroup      string   `json:"caddyGroup"`
	TerminalEnabled bool     `json:"terminalEnabled"`
	ReleaseBaseURL  string   `json:"releaseBaseUrl"`
}

func Default() Config {
	return Config{
		Listen:          "127.0.0.1:8787",
		DataDir:         "/var/lib/serverpal",
		AgentSocket:     "/run/serverpal/agent.sock",
		AgentGroup:      "serverpal",
		AgentTokenFile:  "/etc/serverpal/agent.token",
		AllowedRoots:    []string{"/srv/serverpal/apps"},
		ProjectUser:     "serverpal-app",
		ProjectHome:     "/var/lib/serverpal/app-home",
		PM2Binary:       "/opt/serverpal/pm2-runtime/bin/pm2",
		PM2Home:         "/var/lib/serverpal/pm2",
		NodeBinary:      "/opt/serverpal/node/current/bin/node",
		CaddyBinary:     "/usr/bin/caddy",
		CaddyConfig:     "/etc/caddy/Caddyfile",
		CaddySitesDir:   "/etc/caddy/sites.d",
		CaddyGroup:      "caddy",
		TerminalEnabled: true,
	}
}

func Dev(base string) Config {
	cfg := Default()
	cfg.DataDir = filepath.Join(base, ".dev", "data")
	cfg.AgentSocket = filepath.Join(base, ".dev", "agent.sock")
	cfg.AgentGroup = ""
	cfg.AgentTokenFile = filepath.Join(base, ".dev", "agent.token")
	cfg.AllowedRoots = []string{base}
	cfg.ProjectUser = ""
	cfg.ProjectHome = filepath.Join(base, ".dev", "home")
	cfg.PM2Home = filepath.Join(base, ".dev", "pm2")
	cfg.CaddySitesDir = filepath.Join(base, ".dev", "caddy", "sites.d")
	cfg.CaddyConfig = filepath.Join(base, ".dev", "caddy", "Caddyfile")
	cfg.CaddyGroup = ""
	return cfg
}

func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Listen == "" || c.DataDir == "" || c.AgentSocket == "" || c.AgentTokenFile == "" {
		return errors.New("listen, dataDir, agentSocket and agentTokenFile are required")
	}
	if len(c.AllowedRoots) == 0 {
		return errors.New("at least one allowed root is required")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("listen must be a host:port address: %w", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("web service must listen on a loopback address; expose it through Caddy")
	}
	for _, root := range c.AllowedRoots {
		if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
			return fmt.Errorf("allowed root must be absolute: %s", root)
		}
	}
	for name, value := range map[string]string{
		"dataDir": c.DataDir, "agentSocket": c.AgentSocket, "agentTokenFile": c.AgentTokenFile,
		"projectHome": c.ProjectHome, "pm2Binary": c.PM2Binary, "pm2Home": c.PM2Home,
		"nodeBinary": c.NodeBinary, "caddyBinary": c.CaddyBinary, "caddyConfig": c.CaddyConfig,
		"caddySitesDir": c.CaddySitesDir,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	return nil
}

func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "serverpal.db")
}

func Write(path string, cfg Config, mode os.FileMode) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".serverpal-config-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
