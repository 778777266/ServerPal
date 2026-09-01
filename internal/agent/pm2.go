package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/config"
)

var (
	safeName      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	safeNPMScript = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,63}$`)
	safeEnvKey    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	semverLine    = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
)

type PM2Manager struct {
	cfg   config.Config
	guard *PathGuard
}

func NewPM2Manager(cfg config.Config, guard *PathGuard) *PM2Manager {
	return &PM2Manager{cfg: cfg, guard: guard}
}

func (m *PM2Manager) Available() bool {
	info, err := os.Stat(m.cfg.PM2Binary)
	return err == nil && !info.IsDir()
}

func (m *PM2Manager) Version(ctx context.Context) string {
	if !m.Available() {
		return "unavailable"
	}
	output, err := m.run(ctx, "--version")
	if err != nil {
		return "unavailable"
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(lines[index])
		if semverLine.MatchString(candidate) {
			return candidate
		}
	}
	return "unavailable"
}

func (m *PM2Manager) List(ctx context.Context) ([]Process, error) {
	if !m.Available() {
		return nil, errors.New("PM2 is not installed")
	}
	output, err := m.run(ctx, "jlist")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name  string `json:"name"`
		PM2ID int    `json:"pm_id"`
		PID   int    `json:"pid"`
		Monit struct {
			CPU    float64 `json:"cpu"`
			Memory int64   `json:"memory"`
		} `json:"monit"`
		Env struct {
			Status      string `json:"status"`
			Restarts    int    `json:"restart_time"`
			UptimeMilli int64  `json:"pm_uptime"`
		} `json:"pm2_env"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		start, end := bytes.IndexByte(output, '['), bytes.LastIndexByte(output, ']')
		if start < 0 || end <= start || json.Unmarshal(output[start:end+1], &raw) != nil {
			return nil, fmt.Errorf("parse PM2 process list: %w", err)
		}
	}
	processes := make([]Process, 0, len(raw))
	for _, item := range raw {
		processes = append(processes, Process{
			Name:       item.Name,
			PM2ID:      item.PM2ID,
			PID:        item.PID,
			Status:     item.Env.Status,
			CPU:        item.Monit.CPU,
			Memory:     item.Monit.Memory,
			Restarts:   item.Env.Restarts,
			UptimeUnix: item.Env.UptimeMilli / 1000,
		})
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].Name < processes[j].Name })
	return processes, nil
}

func (m *PM2Manager) Action(ctx context.Context, request ProjectAction) error {
	if !m.Available() {
		return errors.New("PM2 is not installed")
	}
	if !safeName.MatchString(request.Project.PM2Name) {
		return errors.New("invalid PM2 process name")
	}
	switch request.Action {
	case "start":
		file, err := m.writeEcosystem(ctx, request.Project)
		if err != nil {
			return err
		}
		if _, err := m.run(ctx, "start", file, "--only", request.Project.PM2Name, "--update-env"); err != nil {
			return err
		}
	case "restart", "reload":
		file, err := m.writeEcosystem(ctx, request.Project)
		if err != nil {
			return err
		}
		command := "startOrRestart"
		if request.Action == "reload" {
			command = "startOrReload"
		}
		if _, err := m.run(ctx, command, file, "--only", request.Project.PM2Name, "--update-env"); err != nil {
			return err
		}
	case "stop", "delete":
		if _, err := m.run(ctx, request.Action, request.Project.PM2Name); err != nil {
			return err
		}
	default:
		return errors.New("unsupported PM2 action")
	}
	_, _ = m.run(ctx, "save", "--force")
	return nil
}

func (m *PM2Manager) Logs(ctx context.Context, processName string, lines int) (string, error) {
	if !safeName.MatchString(processName) {
		return "", errors.New("invalid PM2 process name")
	}
	if lines < 1 || lines > 2000 {
		lines = 300
	}
	output, err := m.run(ctx, "jlist")
	if err != nil {
		return "", err
	}
	var raw []struct {
		Name string `json:"name"`
		Env  struct {
			Out string `json:"pm_out_log_path"`
			Err string `json:"pm_err_log_path"`
		} `json:"pm2_env"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		start, end := bytes.IndexByte(output, '['), bytes.LastIndexByte(output, ']')
		if start < 0 || end <= start || json.Unmarshal(output[start:end+1], &raw) != nil {
			return "", err
		}
	}
	for _, item := range raw {
		if item.Name != processName {
			continue
		}
		outPath, outPathErr := m.safeLogPath(item.Env.Out)
		errPath, errPathErr := m.safeLogPath(item.Env.Err)
		if outPathErr != nil || errPathErr != nil {
			return "", errors.New("PM2 returned a log path outside its managed directory")
		}
		out, _ := m.tailProjectLog(ctx, outPath, lines)
		errOut, _ := m.tailProjectLog(ctx, errPath, lines)
		if errOut != "" {
			return out + "\n[stderr]\n" + errOut, nil
		}
		return out, nil
	}
	return "", errors.New("PM2 process not found")
}

func (m *PM2Manager) safeLogPath(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid PM2 log path")
	}
	root, err := canonicalPath(m.cfg.PM2Home)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || !within(root, resolved) {
		return "", errors.New("PM2 log path is outside PM2_HOME")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("PM2 log path is not a regular file")
	}
	return resolved, nil
}

func (m *PM2Manager) writeEcosystem(ctx context.Context, project ProjectSpec) (string, error) {
	if err := m.validateProject(project); err != nil {
		return "", err
	}
	root, err := secureOpen(m.guard, project.RootPath, "")
	if err != nil {
		return "", err
	}
	rootInfo, err := root.Stat()
	root.Close()
	if err != nil || !rootInfo.IsDir() {
		return "", errors.New("project root is not a directory")
	}
	env := make(map[string]string, len(project.Env)+2)
	for key, value := range project.Env {
		if !safeEnvKey.MatchString(key) || len(value) > 16*1024 || strings.ContainsRune(value, 0) {
			return "", fmt.Errorf("invalid environment variable: %s", key)
		}
		env[key] = value
	}
	if _, ok := env["NODE_ENV"]; !ok {
		env["NODE_ENV"] = "production"
	}
	if _, ok := env["PORT"]; !ok {
		env["PORT"] = strconv.Itoa(project.Port)
	}

	app := map[string]any{
		"name":               project.PM2Name,
		"cwd":                project.RootPath,
		"instances":          project.Instances,
		"autorestart":        true,
		"watch":              false,
		"kill_timeout":       8000,
		"listen_timeout":     10000,
		"restart_delay":      1500,
		"max_restarts":       10,
		"min_uptime":         "10s",
		"merge_logs":         true,
		"time":               true,
		"env":                env,
		"source_map_support": true,
	}
	if project.Runtime == "npm" {
		npmBinary := filepath.Join(filepath.Dir(project.NodeBinary), "npm")
		if _, err := os.Stat(npmBinary); err != nil {
			located, lookupErr := exec.LookPath("npm")
			if lookupErr != nil {
				return "", errors.New("npm binary was not found")
			}
			npmBinary = located
		}
		app["script"] = npmBinary
		app["args"] = []string{"run", project.NPMScript}
		app["interpreter"] = "none"
		app["exec_mode"] = "fork"
		app["instances"] = 1
	} else {
		entry, err := m.guard.Resolve(project.RootPath, project.Entry, true)
		if err != nil {
			return "", fmt.Errorf("resolve entry file: %w", err)
		}
		app["script"] = entry
		app["interpreter"] = project.NodeBinary
		if project.Instances > 1 {
			app["exec_mode"] = "cluster"
		} else {
			app["exec_mode"] = "fork"
		}
	}

	dir := filepath.Join(m.cfg.PM2Home, "ecosystems")
	file := filepath.Join(dir, "project-"+project.ID+".json")
	b, err := json.MarshalIndent(map[string]any{"apps": []any{app}}, "", "  ")
	if err != nil {
		return "", err
	}
	if m.cfg.ProjectUser != "" {
		if err := m.writeProjectFile(ctx, dir, file, append(b, '\n')); err != nil {
			return "", err
		}
		return file, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if err := writeAtomic(file, append(b, '\n'), 0o640); err != nil {
		return "", err
	}
	return file, nil
}

func (m *PM2Manager) writeProjectFile(ctx context.Context, directory, destination string, data []byte) error {
	if _, err := m.runAsProjectUser(ctx, nil, "/usr/bin/mkdir", "-p", directory); err != nil {
		return err
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.tmp-%x", destination, random)
	defer m.runAsProjectUser(context.Background(), nil, "/usr/bin/rm", "-f", temporary)
	if _, err := m.runAsProjectUser(ctx, data, "/usr/bin/tee", temporary); err != nil {
		return err
	}
	if _, err := m.runAsProjectUser(ctx, nil, "/usr/bin/chmod", "0640", temporary); err != nil {
		return err
	}
	_, err := m.runAsProjectUser(ctx, nil, "/usr/bin/mv", "-f", temporary, destination)
	return err
}

func (m *PM2Manager) validateProject(project ProjectSpec) error {
	if !safeName.MatchString(project.ID) || !safeName.MatchString(project.PM2Name) {
		return errors.New("invalid project identifier")
	}
	if project.Runtime != "node" && project.Runtime != "npm" {
		return errors.New("runtime must be node or npm")
	}
	if project.Runtime == "node" && (project.Entry == "" || filepath.IsAbs(project.Entry)) {
		return errors.New("Node entry must be a relative file path")
	}
	if project.Runtime == "npm" && !safeNPMScript.MatchString(project.NPMScript) {
		return errors.New("invalid npm script name")
	}
	if !filepath.IsAbs(project.NodeBinary) || strings.ContainsRune(project.NodeBinary, 0) {
		return errors.New("Node binary must be an absolute path")
	}
	info, err := os.Stat(project.NodeBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("Node binary does not exist or is not executable")
	}
	if project.Port < 1024 || project.Port > 65535 {
		return errors.New("port must be between 1024 and 65535")
	}
	if project.Instances < 1 || project.Instances > 32 {
		return errors.New("instances must be between 1 and 32")
	}
	return nil
}

func (m *PM2Manager) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if m.cfg.ProjectUser == "" {
		cmd = exec.CommandContext(ctx, m.cfg.PM2Binary, args...)
		cmd.Env = append(os.Environ(), "PM2_HOME="+m.cfg.PM2Home, "HOME="+m.cfg.ProjectHome)
	} else {
		commandPath := strings.Join([]string{filepath.Dir(m.cfg.PM2Binary), filepath.Dir(m.cfg.NodeBinary), "/usr/local/bin", "/usr/bin", "/bin"}, ":")
		runArgs := []string{"-u", m.cfg.ProjectUser, "--", "/usr/bin/env", "PM2_HOME=" + m.cfg.PM2Home, "HOME=" + m.cfg.ProjectHome,
			"PATH=" + commandPath, m.cfg.PM2Binary}
		runArgs = append(runArgs, args...)
		cmd = exec.CommandContext(ctx, "/usr/sbin/runuser", runArgs...)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if len(message) > 4000 {
			message = message[len(message)-4000:]
		}
		return nil, fmt.Errorf("PM2 command failed: %s: %w", message, err)
	}
	return output.Bytes(), nil
}

func (m *PM2Manager) runAsProjectUser(ctx context.Context, input []byte, executable string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	runArgs := []string{"-u", m.cfg.ProjectUser, "--", executable}
	runArgs = append(runArgs, args...)
	cmd := exec.CommandContext(ctx, "/usr/sbin/runuser", runArgs...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if len(message) > 4000 {
			message = message[len(message)-4000:]
		}
		return nil, fmt.Errorf("project user command failed: %s: %w", message, err)
	}
	return output.Bytes(), nil
}

func (m *PM2Manager) tailProjectLog(ctx context.Context, path string, lines int) (string, error) {
	if m.cfg.ProjectUser == "" {
		return tailLines(path, lines)
	}
	output, err := m.runAsProjectUser(ctx, nil, "/usr/bin/tail", "-n", strconv.Itoa(lines), "--", path)
	if len(output) > 2<<20 {
		output = output[len(output)-(2<<20):]
	}
	return string(output), err
}

func tailLines(path string, count int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	const maxBytes = int64(2 << 20)
	start := stat.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, 0); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lines := make([]string, 0, count)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > count {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n"), scanner.Err()
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".serverpal-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
