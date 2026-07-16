package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "goth_session"

var errInvalidCredentials = errors.New("invalid credentials")
var errEmailUnavailable = errors.New("email unavailable")

var passwordDigit = regexp.MustCompile(`[0-9]`)

type user struct {
	ID          int64
	Email       string
	DisplayName string
}

type userContextKey struct{}
type csrfContextKey struct{}

type authService struct {
	db     *sql.DB
	ttl    time.Duration
	secure bool
	now    func() time.Time
	random func([]byte) (int, error)
}

func newAuthService(db *sql.DB, ttl time.Duration, secure bool) *authService {
	return &authService{db: db, ttl: ttl, secure: secure, now: time.Now, random: rand.Read}
}

func (s *authService) ensureUser(ctx context.Context, email, displayName, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return errors.New("AUTH_EMAIL and AUTH_PASSWORD are required")
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = email
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	// Existing databases contain the original single local account (id 1). Claim
	// that row on first authentication setup so existing application data keeps
	// its owner and does not become visible through a different account.
	result, err := s.db.ExecContext(ctx, `UPDATE users SET email=?,display_name=?,password_hash=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=(SELECT id FROM users WHERE password_hash IS NULL ORDER BY id LIMIT 1)`, email, displayName, string(hash))
	if err != nil {
		return fmt.Errorf("initialize authentication user: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect authentication user update: %w", err)
	}
	if updated > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) VALUES(?,?,?)
		ON CONFLICT(email) DO UPDATE SET display_name=excluded.display_name,password_hash=excluded.password_hash,updated_at=CURRENT_TIMESTAMP`, email, displayName, string(hash))
	if err != nil {
		return fmt.Errorf("store authentication user: %w", err)
	}
	return nil
}

func (s *authService) authenticate(ctx context.Context, email, password string) (user, error) {
	var u user
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,email,display_name,password_hash FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.DisplayName, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return user{}, fmt.Errorf("load user: %w", err)
	}
	// Always perform a bcrypt comparison so unknown accounts do not have a
	// meaningfully cheaper authentication path.
	encoded := hash.String
	if !hash.Valid {
		encoded = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5uC3I8bB9zZ9Jrj5YvWf8l5E2M8cZzK"
	}
	if bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) != nil || !hash.Valid {
		return user{}, errInvalidCredentials
	}
	return u, nil
}

func validateRegistration(email, password, confirmation string) map[string]string {
	errorsByField := make(map[string]string)
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		errorsByField["email"] = "Podaj prawidłowy adres e-mail."
	}
	if len(password) < 12 || len(password) > 72 || strings.ToLower(password) == password || strings.ToUpper(password) == password || !passwordDigit.MatchString(password) {
		errorsByField["password"] = "Hasło musi mieć 12–72 znaki oraz zawierać małą i wielką literę i cyfrę."
	}
	if confirmation != password {
		errorsByField["confirmation"] = "Hasła nie są identyczne."
	}
	return errorsByField
}

func (s *authService) register(ctx context.Context, email, password string) (user, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return user{}, fmt.Errorf("hash password: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) VALUES(?,?,?)`, email, email, string(hash))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return user{}, errEmailUnavailable
		}
		return user{}, fmt.Errorf("store user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return user{}, fmt.Errorf("read user id: %w", err)
	}
	return user{ID: id, Email: email, DisplayName: email}, nil
}

func (s *authService) createSession(ctx context.Context, userID int64) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := s.random(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expires := s.now().UTC().Add(s.ttl)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_sessions(user_id,token_hash,expires_at) VALUES(?,?,?)`, userID, hash[:], expires.Format(time.RFC3339Nano)); err != nil {
		return "", time.Time{}, fmt.Errorf("store session: %w", err)
	}
	return token, expires, nil
}

func (s *authService) userForToken(ctx context.Context, token string) (user, error) {
	if token == "" {
		return user{}, errInvalidCredentials
	}
	hash := sha256.Sum256([]byte(token))
	var u user
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.display_name,s.expires_at
		FROM user_sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.revoked_at IS NULL`, hash[:]).Scan(&u.ID, &u.Email, &u.DisplayName, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return user{}, errInvalidCredentials
	}
	if err != nil {
		return user{}, fmt.Errorf("load session: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return user{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if !s.now().UTC().Before(expiresAt) {
		_, _ = s.db.ExecContext(ctx, `UPDATE user_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, s.now().UTC().Format(time.RFC3339Nano), hash[:])
		return user{}, errInvalidCredentials
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE user_sessions SET last_seen_at=? WHERE token_hash=?`, s.now().UTC().Format(time.RFC3339Nano), hash[:])
	return u, nil
}

func (s *authService) revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, s.now().UTC().Format(time.RFC3339Nano), hash[:])
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *authService) cookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(expires.Sub(s.now()).Seconds())}
}

func csrfToken(sessionToken string) string {
	hash := sha256.Sum256([]byte("logout-csrf:" + sessionToken))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func validCSRF(sessionToken, submitted string) bool {
	want, err := base64.RawURLEncoding.DecodeString(csrfToken(sessionToken))
	if err != nil {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(submitted)
	return err == nil && len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1
}

func currentUser(ctx context.Context) (user, bool) {
	u, ok := ctx.Value(userContextKey{}).(user)
	return u, ok
}

func currentCSRF(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}
