package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/serverpal/serverpal/internal/security"
	"github.com/serverpal/serverpal/internal/store"
)

const (
	sessionCookieName      = "serverpal_session"
	maxLoginLimiterEntries = 10000
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,31}$`)

type sessionContextKey struct{}

type loginAttempt struct {
	Failures int
	Since    time.Time
	Blocked  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
	slots    chan struct{}
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]loginAttempt), slots: make(chan struct{}, 2)}
}

func (l *loginLimiter) beginVerification() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *loginLimiter) endVerification() { <-l.slots }

func (l *loginLimiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	attempt, exists := l.attempts[key]
	if !exists && len(l.attempts) >= maxLoginLimiterEntries {
		for candidate, value := range l.attempts {
			if now.After(value.Blocked) && (value.Since.IsZero() || now.Sub(value.Since) > 10*time.Minute) {
				delete(l.attempts, candidate)
			}
		}
		if len(l.attempts) >= maxLoginLimiterEntries {
			return false
		}
	}
	if now.Before(attempt.Blocked) {
		return false
	}
	if attempt.Since.IsZero() || now.Sub(attempt.Since) > 10*time.Minute {
		delete(l.attempts, key)
	}
	return true
}

func (l *loginLimiter) failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	attempt := l.attempts[key]
	if attempt.Since.IsZero() || now.Sub(attempt.Since) > 10*time.Minute {
		attempt = loginAttempt{Since: now}
	}
	attempt.Failures++
	if attempt.Failures >= 5 {
		attempt.Blocked = now.Add(15 * time.Minute)
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.readSession(r)
	if !ok {
		apiJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	apiJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          map[string]string{"id": session.UserID, "username": session.Username},
		"csrfToken":     session.CSRFToken,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !sameOrigin(r) {
		apiError(w, http.StatusForbidden, "origin rejected")
		return
	}
	key := clientIP(r)
	if !s.limiter.allowed(key) {
		apiError(w, http.StatusTooManyRequests, "登录失败次数过多，请稍后再试")
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeRequest(w, r, &request, 32*1024) {
		return
	}
	if !s.limiter.beginVerification() {
		apiError(w, http.StatusTooManyRequests, "登录请求过多，请稍后再试")
		return
	}
	defer s.limiter.endVerification()
	request.Username = strings.TrimSpace(request.Username)
	user, err := s.store.UserByUsername(r.Context(), request.Username)
	hash := s.dummyHash
	if err == nil {
		hash = user.PasswordHash
	}
	valid := len(request.Password) <= 1024 && security.VerifyPassword(hash, request.Password)
	if err != nil || !valid || !usernamePattern.MatchString(request.Username) {
		s.limiter.failure(key)
		apiError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	rawToken, tokenErr := security.RandomToken(32)
	csrfToken, csrfErr := security.RandomToken(24)
	sessionID, idErr := security.RandomToken(18)
	if tokenErr != nil || csrfErr != nil || idErr != nil {
		apiError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	session := store.Session{
		ID: sessionID, UserID: user.ID, Username: user.Username,
		TokenHash: security.TokenHash(rawToken), CSRFToken: csrfToken, ExpiresAt: expires,
	}
	if err := s.store.CreateSession(r.Context(), session); err != nil {
		apiError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.limiter.success(key)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: rawToken, Path: "/", HttpOnly: true,
		Secure: requestIsSecure(r), SameSite: http.SameSiteStrictMode,
		Expires: expires, MaxAge: 86400,
	})
	_ = s.store.DeleteExpiredSessions(context.Background())
	_ = s.store.AddAudit(context.Background(), store.AuditLog{
		UserID: user.ID, Username: user.Username, Action: "auth.login", Target: "session", IP: key,
	})
	apiJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          map[string]string{"id": user.ID, "username": user.Username},
		"csrfToken":     csrfToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = s.store.DeleteSession(r.Context(), security.TokenHash(cookie.Value))
	}
	s.audit(r, "auth.logout", "session", "")
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: requestIsSecure(r), SameSite: http.SameSiteStrictMode,
		Expires: time.Unix(0, 0), MaxAge: -1,
	})
	apiJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.readSession(r)
		if !ok {
			apiError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		if !sameOrigin(r) {
			apiError(w, http.StatusForbidden, "origin rejected")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			provided := r.Header.Get("X-CSRF-Token")
			if len(provided) != len(session.CSRFToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
				apiError(w, http.StatusForbidden, "CSRF validation failed")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, session)))
	})
}

func (s *Server) readSession(r *http.Request) (store.Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || len(cookie.Value) < 32 {
		return store.Session{}, false
	}
	session, err := s.store.SessionByTokenHash(r.Context(), security.TokenHash(cookie.Value))
	return session, err == nil
}

func sessionFromContext(ctx context.Context) (store.Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(store.Session)
	return session, ok
}

func requestIsSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
