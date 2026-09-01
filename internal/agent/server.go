package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/config"
)

type Server struct {
	cfg      config.Config
	version  string
	token    string
	guard    *PathGuard
	metrics  *MetricsCollector
	pm2      *PM2Manager
	caddy    *CaddyManager
	files    *FileService
	terminal *TerminalService
}

func NewServer(cfg config.Config, version string) (*Server, error) {
	tokenBytes, err := os.ReadFile(cfg.AgentTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read agent token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		return nil, errors.New("agent token is invalid")
	}
	guard, err := NewPathGuard(cfg.AllowedRoots)
	if err != nil {
		return nil, err
	}
	diskPath := "/"
	if len(cfg.AllowedRoots) > 0 {
		diskPath = cfg.AllowedRoots[0]
	}
	files, err := NewFileService(guard, cfg.ProjectUser)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:      cfg,
		version:  version,
		token:    token,
		guard:    guard,
		metrics:  NewMetricsCollector(diskPath),
		pm2:      NewPM2Manager(cfg, guard),
		caddy:    NewCaddyManager(cfg),
		files:    files,
		terminal: NewTerminalService(cfg, guard),
	}, nil
}

func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.AgentSocket), 0o750); err != nil {
		return err
	}
	if info, err := os.Lstat(s.cfg.AgentSocket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("agent socket path exists and is not a socket")
		}
		if err := os.Remove(s.cfg.AgentSocket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", s.cfg.AgentSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(s.cfg.AgentSocket)
	if err := os.Chmod(s.cfg.AgentSocket, 0o660); err != nil {
		return err
	}
	if s.cfg.AgentGroup != "" {
		group, lookupErr := user.LookupGroup(s.cfg.AgentGroup)
		if lookupErr != nil {
			return fmt.Errorf("lookup agent group: %w", lookupErr)
		}
		gid, convertErr := strconv.Atoi(group.Gid)
		if convertErr != nil {
			return convertErr
		}
		if err := os.Chown(s.cfg.AgentSocket, -1, gid); err != nil {
			return err
		}
	}

	httpServer := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) })
	mux.HandleFunc("/v1/metrics", s.handleMetrics)
	mux.HandleFunc("/v1/system", s.handleSystem)
	mux.HandleFunc("/v1/project/root", s.handleProjectRoot)
	mux.HandleFunc("/v1/pm2/list", s.handlePM2List)
	mux.HandleFunc("/v1/pm2/action", s.handlePM2Action)
	mux.HandleFunc("/v1/pm2/logs", s.handlePM2Logs)
	mux.HandleFunc("/v1/domain/apply", s.handleDomainApply)
	mux.HandleFunc("/v1/domain/remove", s.handleDomainRemove)
	mux.HandleFunc("/v1/domain/status", s.handleDomainStatus)
	mux.HandleFunc("/v1/files/list", s.handleFileList)
	mux.HandleFunc("/v1/files/content", s.handleFileContent)
	mux.HandleFunc("/v1/files/upload", s.handleFileUpload)
	mux.HandleFunc("/v1/files/download", s.handleFileDownload)
	mux.HandleFunc("/v1/files/mkdir", s.handleFileMkdir)
	mux.HandleFunc("/v1/files/rename", s.handleFileRename)
	mux.HandleFunc("/v1/files/trash", s.handleFileTrash)
	mux.Handle("/v1/terminal", s.terminal)
	return s.requireToken(mux)
}

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-ServerPal-Agent-Token")
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "agent authentication failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.metrics.Collect())
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, DetectSystem(r.Context(), s.cfg, s.version))
}

func (s *Server) handleProjectRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &request, 32*1024) {
		return
	}
	if err := s.files.EnsureProjectRoot(request.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePM2List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	processes, err := s.pm2.List(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, processes)
}

func (s *Server) handlePM2Action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request ProjectAction
	if !decodeJSON(w, r, &request, 512*1024) {
		return
	}
	if err := s.pm2.Action(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePM2Logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	logs, err := s.pm2.Logs(r.Context(), r.URL.Query().Get("name"), lines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleDomainApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request DomainSpec
	if !decodeJSON(w, r, &request, 64*1024) {
		return
	}
	if err := s.caddy.Apply(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDomainRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &request, 16*1024) {
		return
	}
	if err := s.caddy.Remove(r.Context(), request.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDomainStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.caddy.CertificateStatus(r.Context(), r.URL.Query().Get("hostname")))
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := s.files.List(r.URL.Query().Get("root"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	root, path := r.URL.Query().Get("root"), r.URL.Query().Get("path")
	switch r.Method {
	case http.MethodGet:
		content, mode, err := s.files.ReadContent(root, path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": string(content), "mode": mode.String()})
	case http.MethodPut:
		content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEditableFile+1))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
			return
		}
		if err := s.files.WriteContent(root, path, content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.files.Upload(r.URL.Query().Get("root"), r.URL.Query().Get("path"), http.MaxBytesReader(w, r.Body, maxUploadFile+1)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	file, info, err := s.files.OpenDownload(r.URL.Query().Get("root"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Name()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, file)
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct{ Root, Path string }
	if !decodeJSON(w, r, &request, 32*1024) {
		return
	}
	if err := s.files.Mkdir(request.Root, request.Path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct{ Root, From, To string }
	if !decodeJSON(w, r, &request, 32*1024) {
		return
	}
	if err := s.files.Rename(request.Root, request.From, request.To); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleFileTrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct{ Root, Path string }
	if !decodeJSON(w, r, &request, 32*1024) {
		return
	}
	destination, err := s.files.Trash(request.Root, request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "trashPath": destination})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
