package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/serverpal/serverpal/internal/agent"
	"github.com/serverpal/serverpal/internal/api"
	"github.com/serverpal/serverpal/internal/config"
	"github.com/serverpal/serverpal/internal/security"
	"github.com/serverpal/serverpal/internal/store"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

var validUsername = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,31}$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "serverpal:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "serve":
		return runWeb(args[1:])
	case "agent":
		return runAgent(args[1:])
	case "init":
		return runInit(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "reset-password":
		return runResetPassword(args[1:])
	case "dev":
		return runDev(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("ServerPal %s (%s, %s)\n", version, commit, buildDate)
		return nil
	case "help", "--help", "-h":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runWeb(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer database.Close()
	agentClient, err := agent.NewClient(cfg)
	if err != nil {
		return err
	}
	dummyHash, err := security.HashPassword("not-a-real-serverpal-password")
	if err != nil {
		return err
	}
	logger := newLogger()
	server := api.NewServer(cfg, database, agentClient, version, logger, dummyHash)
	return server.Serve(signalContext())
}

func runAgent(args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	server, err := agent.NewServer(cfg, version)
	if err != nil {
		return err
	}
	newLogger().Info("privileged agent starting", "socket", cfg.AgentSocket)
	return server.Serve(signalContext())
}

func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	username := flags.String("username", "admin", "initial administrator username")
	password := flags.String("password", "", "initial administrator password")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !validUsername.MatchString(*username) {
		return errors.New("username must be 3-32 characters using letters, numbers, dot, underscore or dash")
	}
	cfg, err := ensureConfig(*configPath)
	if err != nil {
		return err
	}
	if err := ensureRuntimeFiles(cfg); err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer database.Close()
	count, err := database.UserCount(context.Background())
	if err != nil {
		return err
	}
	if count > 0 {
		fmt.Println("ServerPal is already initialized; no credentials were changed.")
		return nil
	}
	generated := false
	if *password == "" {
		*password, err = security.RandomToken(18)
		if err != nil {
			return err
		}
		generated = true
	}
	hash, err := security.HashPassword(*password)
	if err != nil {
		return err
	}
	id, err := security.RandomID(12)
	if err != nil {
		return err
	}
	if err := database.CreateUser(context.Background(), id, *username, hash); err != nil {
		return err
	}
	fmt.Printf("Administrator: %s\n", *username)
	if generated {
		fmt.Printf("Generated password: %s\n", *password)
	} else {
		fmt.Println("Administrator password configured.")
	}
	return nil
}

func runMigrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer database.Close()
	fmt.Println("Database migration completed.")
	return nil
}

func runResetPassword(args []string) error {
	flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	username := flags.String("username", "admin", "administrator username")
	password := flags.String("password", "", "new password")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !validUsername.MatchString(*username) {
		return errors.New("username is invalid")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *password == "" {
		*password, err = security.RandomToken(18)
		if err != nil {
			return err
		}
		fmt.Printf("Generated password: %s\n", *password)
	}
	hash, err := security.HashPassword(*password)
	if err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.UpdatePassword(context.Background(), *username, hash); err != nil {
		return err
	}
	fmt.Println("Password reset completed; existing sessions were revoked.")
	return nil
}

func runDoctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := flags.String("config", "/etc/serverpal/config.json", "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	database.Close()
	client, err := agent.NewClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	status, err := client.System(ctx)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	fmt.Printf("Database: ok\nAgent: %s\nNode: %s\nPM2: %s\nCaddy: %s\n", status.AgentVersion, status.Node, status.PM2, status.Caddy)
	return nil
}

func runDev(args []string) error {
	flags := flag.NewFlagSet("dev", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8787", "web listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	base, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg := config.Dev(base)
	cfg.Listen = *listen
	configPath := filepath.Join(base, ".dev", "config.json")
	if err := config.Write(configPath, cfg, 0o600); err != nil {
		return err
	}
	if err := ensureRuntimeFiles(cfg); err != nil {
		return err
	}
	database, err := store.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	count, err := database.UserCount(context.Background())
	if err != nil {
		return err
	}
	if count == 0 {
		hash, _ := security.HashPassword("serverpal-dev-password")
		id, _ := security.RandomID(12)
		if err := database.CreateUser(context.Background(), id, "admin", hash); err != nil {
			return err
		}
		fmt.Println("Development login: admin / serverpal-dev-password")
	}
	database.Close()
	ctx, cancel := context.WithCancel(signalContext())
	defer cancel()
	agentServer, err := agent.NewServer(cfg, version)
	if err != nil {
		return err
	}
	agentErr := make(chan error, 1)
	go func() { agentErr <- agentServer.Serve(ctx) }()
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(cfg.AgentSocket); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	webErr := make(chan error, 1)
	go func() { webErr <- runWeb([]string{"--config", configPath}) }()
	select {
	case err := <-agentErr:
		return err
	case err := <-webErr:
		return err
	case <-ctx.Done():
		return nil
	}
}

func ensureConfig(path string) (config.Config, error) {
	if _, err := os.Stat(path); err == nil {
		return config.Load(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return config.Config{}, err
	}
	cfg := config.Default()
	if err := config.Write(path, cfg, 0o640); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func ensureRuntimeFiles(cfg config.Config) error {
	for _, directory := range append([]string{cfg.DataDir, cfg.ProjectHome, cfg.PM2Home, filepath.Dir(cfg.AgentSocket)}, cfg.AllowedRoots...) {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return err
		}
	}
	if _, err := os.Stat(cfg.AgentTokenFile); errors.Is(err, os.ErrNotExist) {
		token, tokenErr := security.RandomToken(48)
		if tokenErr != nil {
			return tokenErr
		}
		if err := os.MkdirAll(filepath.Dir(cfg.AgentTokenFile), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(cfg.AgentTokenFile, []byte(token+"\n"), 0o640); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func signalContext() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func usage() error {
	fmt.Println(`ServerPal commands:
  serverpal init             initialize configuration and administrator
  serverpal serve            run the unprivileged web service
  serverpal agent            run the privileged local agent
  serverpal migrate          apply database migrations
  serverpal reset-password   reset an administrator password
  serverpal doctor           verify local services
  serverpal version          print build information
  serverpal dev              run both services for local development`)
	return nil
}
