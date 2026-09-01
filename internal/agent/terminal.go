package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/serverpal/serverpal/internal/config"
)

type TerminalService struct {
	cfg   config.Config
	guard *PathGuard
}

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func NewTerminalService(cfg config.Config, guard *PathGuard) *TerminalService {
	return &TerminalService{cfg: cfg, guard: guard}
}

func (s *TerminalService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.TerminalEnabled {
		http.Error(w, "terminal is disabled", http.StatusForbidden)
		return
	}
	cwd := r.URL.Query().Get("cwd")
	if cwd == "" || !filepath.IsAbs(cwd) || !s.guard.IsAllowed(cwd) {
		http.Error(w, "invalid terminal working directory", http.StatusBadRequest)
		return
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil || !s.guard.IsAllowed(resolved) {
		http.Error(w, "terminal working directory is unavailable", http.StatusBadRequest)
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(64 * 1024)

	cmd := s.shellCommand(r.Context(), resolved)
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 32})
	if err != nil {
		_ = connection.Close(websocket.StatusInternalError, "unable to start terminal")
		return
	}
	defer terminal.Close()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var writeMu sync.Mutex
	readDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := terminal.Read(buffer)
			if n > 0 {
				writeMu.Lock()
				writeErr := connection.Write(ctx, websocket.MessageBinary, buffer[:n])
				writeMu.Unlock()
				if writeErr != nil {
					readDone <- writeErr
					return
				}
			}
			if readErr != nil {
				readDone <- readErr
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		default:
		}
		messageType, payload, err := connection.Read(ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		var message terminalMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		switch message.Type {
		case "input":
			if len(message.Data) <= 32*1024 && !strings.ContainsRune(message.Data, 0) {
				_, _ = io.WriteString(terminal, message.Data)
			}
		case "resize":
			if message.Cols >= 20 && message.Cols <= 400 && message.Rows >= 5 && message.Rows <= 200 {
				_ = pty.Setsize(terminal, &pty.Winsize{Cols: message.Cols, Rows: message.Rows})
			}
		}
	}
}

func (s *TerminalService) shellCommand(ctx context.Context, cwd string) *exec.Cmd {
	var cmd *exec.Cmd
	if s.cfg.ProjectUser == "" {
		cmd = exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc")
	} else {
		commandPath := strings.Join([]string{filepath.Dir(s.cfg.PM2Binary), filepath.Dir(s.cfg.NodeBinary), "/usr/local/bin", "/usr/bin", "/bin"}, ":")
		cmd = exec.CommandContext(ctx, "/usr/sbin/runuser", "-u", s.cfg.ProjectUser, "--", "/usr/bin/env",
			"HOME="+s.cfg.ProjectHome, "PM2_HOME="+s.cfg.PM2Home, "PATH="+commandPath, "TERM=xterm-256color", "COLORTERM=truecolor",
			"/bin/bash", "--noprofile", "--norc")
	}
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HOME="+s.cfg.ProjectHome,
		"PM2_HOME="+s.cfg.PM2Home,
		"SERVERPAL_CWD="+cwd,
		"COLUMNS="+strconv.Itoa(120),
		"LINES="+strconv.Itoa(32),
	)
	return cmd
}
