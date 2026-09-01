package config

import "testing"

func TestConfigRequiresLoopbackControlPlane(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
	cfg.Listen = "0.0.0.0:8787"
	if err := cfg.Validate(); err == nil {
		t.Fatal("public control-plane listen address was accepted")
	}
}

func TestConfigRejectsFilesystemRoot(t *testing.T) {
	cfg := Default()
	cfg.AllowedRoots = []string{"/"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("filesystem root was accepted as an allowed project path")
	}
}
