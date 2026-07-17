package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

type recordingEmailSender struct{ messages []emailMessage }

func (s *recordingEmailSender) Send(_ context.Context, message emailMessage) error {
	s.messages = append(s.messages, message)
	return nil
}

func tokenFromMessage(t *testing.T, message emailMessage) string {
	t.Helper()
	start := strings.Index(message.Text, "http")
	if start < 0 {
		t.Fatal("message has no link")
	}
	link, err := url.Parse(message.Text[start:])
	if err != nil {
		t.Fatal(err)
	}
	return link.Query().Get("token")
}

func recoveryAuth(t *testing.T) (*authService, *recordingEmailSender, *time.Time) {
	t.Helper()
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	sender := &recordingEmailSender{}
	auth := newAuthService(db, time.Hour, false)
	auth.now = func() time.Time { return now }
	auth.mailer = sender
	auth.baseURL = "https://example.test"
	if err := auth.ensureUser(t.Context(), "owner@example.com", "Owner", "StrongPassword1"); err != nil {
		t.Fatal(err)
	}
	return auth, sender, &now
}

func TestPasswordResetIsHashedOneTimeAndRevokesSessions(t *testing.T) {
	auth, sender, _ := recoveryAuth(t)
	u, _ := auth.authenticate(t.Context(), "owner@example.com", "StrongPassword1")
	session, _, err := auth.createSession(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.sendPasswordReset(t.Context(), u.Email); err != nil {
		t.Fatal(err)
	}
	token := tokenFromMessage(t, sender.messages[0])
	var stored []byte
	if err := auth.db.QueryRow(`SELECT token_hash FROM auth_tokens WHERE purpose='password_reset'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(token))
	if string(stored) != string(want[:]) || string(stored) == token {
		t.Fatal("password reset token was not stored only as a hash")
	}
	if err := auth.resetPassword(t.Context(), token, "NewStrongPassword2"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "NewStrongPassword2"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.userForToken(t.Context(), session); !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("old session error = %v", err)
	}
	if err := auth.resetPassword(t.Context(), token, "AnotherStrongPassword3"); !errors.Is(err, errInvalidAuthToken) {
		t.Fatalf("reused token error = %v", err)
	}
}

func TestPasswordResetExpiryRateLimitAndAccountPrivacy(t *testing.T) {
	auth, sender, now := recoveryAuth(t)
	if err := auth.sendPasswordReset(t.Context(), "missing@example.com"); err != nil || len(sender.messages) != 0 {
		t.Fatalf("unknown account = %v, messages=%d", err, len(sender.messages))
	}
	if err := auth.sendPasswordReset(t.Context(), "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := auth.sendPasswordReset(t.Context(), "owner@example.com"); !errors.Is(err, errAuthRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
	token := tokenFromMessage(t, sender.messages[0])
	*now = now.Add(auth.tokenTTL)
	if err := auth.resetPassword(t.Context(), token, "NewStrongPassword2"); !errors.Is(err, errInvalidAuthToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestEmailVerificationAndResendRateLimit(t *testing.T) {
	auth, sender, now := recoveryAuth(t)
	u, _ := auth.authenticate(t.Context(), "owner@example.com", "StrongPassword1")
	if err := auth.sendVerification(t.Context(), u.ID); err != nil {
		t.Fatal(err)
	}
	if err := auth.sendVerification(t.Context(), u.ID); !errors.Is(err, errAuthRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
	token := tokenFromMessage(t, sender.messages[0])
	if err := auth.verifyEmail(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if err := auth.verifyEmail(t.Context(), token); !errors.Is(err, errInvalidAuthToken) {
		t.Fatalf("reused verification error = %v", err)
	}
	*now = now.Add(auth.resendInterval)
	if err := auth.sendVerification(t.Context(), u.ID); err != nil {
		t.Fatalf("verified resend should be a no-op: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("verified account received %d messages", len(sender.messages))
	}
}
