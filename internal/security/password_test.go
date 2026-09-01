package security

import (
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct-horse-battery-staple") {
		t.Fatal("expected password verification to succeed")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestPasswordRejectsShortValue(t *testing.T) {
	if _, err := HashPassword("too-short"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
}

func TestTokenHashIsStableAndDoesNotExposeToken(t *testing.T) {
	token, err := RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	first := TokenHash(token)
	second := TokenHash(token)
	if first != second || first == token {
		t.Fatal("unexpected token hash")
	}
}

func TestRandomIDUsesSafeHexCharacters(t *testing.T) {
	id, err := RandomID(12)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 24 {
		t.Fatalf("unexpected id length: %d", len(id))
	}
	for _, character := range id {
		if !strings.ContainsRune("0123456789abcdef", character) {
			t.Fatalf("unsafe id character: %q", character)
		}
	}
}
