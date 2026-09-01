package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/agent"
	"github.com/serverpal/serverpal/internal/config"
	"github.com/serverpal/serverpal/internal/store"
	"github.com/serverpal/serverpal/internal/webui"
)

type Server struct {
	cfg       config.Config
	store     *store.Store
	agent     *agent.Client
	version   string
	logger    *slog.Logger
	limiter   *loginLimiter
	dummyHash string
}

func NewServer(cfg config.Config, database *store.Store, agentClient *agent.Client, version string, logger *slog.Logger, dummyHash string) *Server {
	return &Server{
		cfg: cfg, store: database, agent: agentClient, version: version, logger: logger,
		limiter: newLoginLimiter(), dummyHash: dummyHash,
	}
}

func (s *Server) Serve(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}
	listener, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	s.logger.Info("web service listening", "address", s.cfg.Listen)
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	root := http.NewServeMux()
	root.HandleFunc("/api/health", s.handleHealth)
	root.HandleFunc("/api/auth/status", s.handleAuthStatus)
	root.HandleFunc("/api/auth/login", s.handleLogin)

	protected := http.NewServeMux()
	protected.HandleFunc("/api/auth/logout", s.handleLogout)
	protected.HandleFunc("/api/overview", s.handleOverview)
	protected.HandleFunc("/api/projects", s.handleProjects)
	protected.HandleFunc("/api/projects/", s.handleProject)
	protected.HandleFunc("/api/domains", s.handleDomains)
	protected.HandleFunc("/api/domains/", s.handleDomain)
	protected.HandleFunc("/api/files", s.handleFiles)
	protected.HandleFunc("/api/files/", s.handleFiles)
	protected.HandleFunc("/api/terminal", s.handleTerminal)
	protected.HandleFunc("/api/audit", s.handleAudit)
	protected.HandleFunc("/api/system", s.handleSystem)
	root.Handle("/api/", s.authenticate(protected))
	root.Handle("/", webui.Handler())
	return requestSecurity(root)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, agentErr := s.agent.Metrics(ctx)
	status := http.StatusOK
	agentStatus := "ok"
	if agentErr != nil {
		status = http.StatusServiceUnavailable
		agentStatus = "unavailable"
	}
	apiJSON(w, status, map[string]any{
		"status": "ok", "agent": agentStatus, "version": s.version,
	})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status, err := s.agent.System(r.Context())
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	apiJSON(w, http.StatusOK, status)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	logs, err := s.store.AuditLogs(r.Context(), 200)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "unable to load audit log")
		return
	}
	apiJSON(w, http.StatusOK, map[string]any{"items": logs})
}

func (s *Server) audit(r *http.Request, action, target, detail string) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return
	}
	log := store.AuditLog{
		UserID: session.UserID, Username: session.Username, Action: action,
		Target: target, Detail: detail, IP: clientIP(r),
	}
	if err := s.store.AddAudit(context.Background(), log); err != nil {
		s.logger.Warn("unable to write audit log", "error", err)
	}
}

func apiJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, message string) {
	apiJSON(w, status, map[string]any{"error": message})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		apiError(w, http.StatusBadRequest, "请求内容无效")
		return false
	}
	return true
}

func requestSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if requestIsSecure(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if net.ParseIP(host).IsLoopback() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			candidate := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	return host
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host) && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

func pathID(prefix, path string) (string, []string, error) {
	trimmed := strings.TrimPrefix(path, prefix)
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", nil, fmt.Errorf("missing identifier")
	}
	return parts[0], parts[1:], nil
}
