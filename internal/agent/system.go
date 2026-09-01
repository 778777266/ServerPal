package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/config"
)

func DetectSystem(ctx context.Context, cfg config.Config, version string) SystemStatus {
	status := SystemStatus{
		AgentVersion: version,
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		Terminal:     cfg.TerminalEnabled,
	}
	status.Node = commandVersion(ctx, cfg.NodeBinary, "--version")
	guard, err := NewPathGuard(cfg.AllowedRoots)
	if err == nil {
		status.PM2 = NewPM2Manager(cfg, guard).Version(ctx)
	} else {
		status.PM2 = "unavailable"
	}
	status.Caddy = commandVersion(ctx, cfg.CaddyBinary, "version")
	status.PM2Available = status.PM2 != "unavailable"
	status.CaddyReady = NewCaddyManager(cfg).Ready()
	return status
}

func commandVersion(ctx context.Context, binary string, args ...string) string {
	versionCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, binary, args...).CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	value := strings.TrimSpace(string(output))
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}
