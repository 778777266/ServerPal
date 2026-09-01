package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/config"
)

var hostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type CaddyManager struct {
	cfg config.Config
}

func NewCaddyManager(cfg config.Config) *CaddyManager {
	return &CaddyManager{cfg: cfg}
}

func ValidateHostname(value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(value))
	if len(host) < 3 || len(host) > 253 || strings.ContainsAny(host, "/\\:@ \t\r\n") || net.ParseIP(host) != nil {
		return "", errors.New("invalid domain name")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", errors.New("a fully qualified domain name is required")
	}
	for _, label := range labels {
		if !hostnameLabel.MatchString(label) {
			return "", errors.New("invalid domain label")
		}
	}
	return host, nil
}

func (m *CaddyManager) Ready() bool {
	if _, err := os.Stat(m.cfg.CaddyBinary); err != nil {
		return false
	}
	b, err := os.ReadFile(m.cfg.CaddyConfig)
	return err == nil && strings.Contains(string(b), m.cfg.CaddySitesDir)
}

func (m *CaddyManager) Apply(ctx context.Context, domain DomainSpec) error {
	if !safeName.MatchString(domain.ID) || !safeName.MatchString(domain.ProjectID) {
		return errors.New("invalid domain identifier")
	}
	host, err := ValidateHostname(domain.Hostname)
	if err != nil {
		return err
	}
	if domain.Port < 1 || domain.Port > 65535 {
		return errors.New("invalid upstream port")
	}
	if !m.Ready() {
		return errors.New("Caddy is not installed or its ServerPal import is missing")
	}
	if err := os.MkdirAll(m.cfg.CaddySitesDir, 0o750); err != nil {
		return err
	}
	address := host
	if !domain.HTTPS {
		address = "http://" + host
	}
	content := fmt.Sprintf("# Managed by ServerPal. Manual edits will be replaced.\n%s {\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:%d\n}\n", address, domain.Port)
	path := m.domainPath(domain.ID)
	previous, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := writeAtomic(path, []byte(content), 0o640); err != nil {
		return err
	}
	m.chownForCaddy(path)
	if err := m.validateAndReload(ctx); err != nil {
		if existed {
			_ = writeAtomic(path, previous, 0o640)
			m.chownForCaddy(path)
		} else {
			_ = os.Remove(path)
		}
		_ = m.reload(ctx)
		return err
	}
	return nil
}

func (m *CaddyManager) Remove(ctx context.Context, id string) error {
	if !safeName.MatchString(id) {
		return errors.New("invalid domain identifier")
	}
	path := m.domainPath(id)
	previous, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := m.validateAndReload(ctx); err != nil {
		_ = writeAtomic(path, previous, 0o640)
		m.chownForCaddy(path)
		_ = m.reload(ctx)
		return err
	}
	return nil
}

func (m *CaddyManager) CertificateStatus(ctx context.Context, hostname string) CertificateStatus {
	host, err := ValidateHostname(hostname)
	if err != nil {
		return CertificateStatus{Status: "error", Error: err.Error()}
	}
	dialer := &net.Dialer{Timeout: 7 * time.Second}
	connection, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, "443"), &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return CertificateStatus{Status: "pending", Error: err.Error()}
	}
	defer connection.Close()
	state := connection.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return CertificateStatus{Status: "pending", Error: "server returned no certificate"}
	}
	cert := state.PeerCertificates[0]
	issuer := cert.Issuer.CommonName
	if issuer == "" && len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0]
	}
	return CertificateStatus{Status: "active", Issuer: issuer, ExpiresAt: &cert.NotAfter}
}

func (m *CaddyManager) domainPath(id string) string {
	return filepath.Join(m.cfg.CaddySitesDir, "serverpal-"+id+".caddy")
}

func (m *CaddyManager) validateAndReload(ctx context.Context) error {
	validateCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(validateCtx, m.cfg.CaddyBinary, "validate", "--config", m.cfg.CaddyConfig, "--adapter", "caddyfile")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Caddy validation failed: %s: %w", strings.TrimSpace(output.String()), err)
	}
	return m.reload(ctx)
}

func (m *CaddyManager) reload(ctx context.Context) error {
	reloadCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(reloadCtx, "/usr/bin/systemctl", "reload", "caddy")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reload Caddy: %s: %w", strings.TrimSpace(output.String()), err)
	}
	return nil
}

func (m *CaddyManager) chownForCaddy(path string) {
	if m.cfg.CaddyGroup == "" {
		return
	}
	group, err := user.LookupGroup(m.cfg.CaddyGroup)
	if err != nil {
		return
	}
	gid, err := strconv.Atoi(group.Gid)
	if err == nil {
		_ = os.Chown(path, -1, gid)
	}
}
