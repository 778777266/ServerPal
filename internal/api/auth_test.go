package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLoginLimiterBoundsUniqueClients(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Now()
	for i := 0; i < maxLoginLimiterEntries; i++ {
		limiter.attempts[strconv.Itoa(i)] = loginAttempt{Failures: 1, Since: now}
	}
	if limiter.allowed("new-client") {
		t.Fatal("new client was accepted after limiter reached its memory bound")
	}
	for key, attempt := range limiter.attempts {
		attempt.Since = now.Add(-11 * time.Minute)
		limiter.attempts[key] = attempt
	}
	if !limiter.allowed("new-client") {
		t.Fatal("expired limiter entries were not reclaimed")
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := requestSecurity(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "https://panel.example.com/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, name := range []string{"Content-Security-Policy", "Permissions-Policy", "Strict-Transport-Security", "X-Content-Type-Options"} {
		if response.Header().Get(name) == "" {
			t.Fatalf("security header %s is missing", name)
		}
	}
}

func TestLoginVerificationConcurrencyIsBounded(t *testing.T) {
	limiter := newLoginLimiter()
	if !limiter.beginVerification() || !limiter.beginVerification() {
		t.Fatal("expected both verification slots to be available")
	}
	if limiter.beginVerification() {
		t.Fatal("third concurrent verification was accepted")
	}
	limiter.endVerification()
	if !limiter.beginVerification() {
		t.Fatal("released verification slot was not reusable")
	}
	limiter.endVerification()
	limiter.endVerification()
}
