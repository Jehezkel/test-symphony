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
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "goth_session"

var errInvalidCredentials = errors.New("invalid credentials")
var errEmailUnavailable = errors.New("email unavailable")
var errInvalidAuthToken = errors.New("invalid authentication token")
var errAuthRateLimited = errors.New("authentication message rate limited")

var passwordDigit = regexp.MustCompile(`[0-9]`)

type user struct {
	ID          int64
	Email       string
	DisplayName string
}

type userContextKey struct{}
type csrfContextKey struct{}

type authService struct {
	db             *sql.DB
	ttl            time.Duration
	secure         bool
	now            func() time.Time
	random         func([]byte) (int, error)
	mailer         emailSender
	baseURL        string
	tokenTTL       time.Duration
	resendInterval time.Duration
}

func newAuthService(db *sql.DB, ttl time.Duration, secure bool) *authService {
	return &authService{db: db, ttl: ttl, secure: secure, now: time.Now, random: rand.Read, mailer: discardEmailSender{}, baseURL: "http://localhost:8080", tokenTTL: 30 * time.Minute, resendInterval: time.Minute}
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

func (s *authService) sendPasswordReset(ctx context.Context, email string) error {
	var u user
	err := s.db.QueryRowContext(ctx, `SELECT id,email,display_name FROM users WHERE email=? AND password_hash IS NOT NULL`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load password reset user: %w", err)
	}
	return s.sendAuthToken(ctx, u, "password_reset", "/reset-password?token=", "Reset hasła")
}

func (s *authService) sendVerification(ctx context.Context, userID int64) error {
	var u user
	var verified sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT id,email,display_name,email_verified_at FROM users WHERE id=?`, userID).Scan(&u.ID, &u.Email, &u.DisplayName, &verified); err != nil {
		return fmt.Errorf("load verification user: %w", err)
	}
	if verified.Valid {
		return nil
	}
	return s.sendAuthToken(ctx, u, "email_verification", "/verify-email?token=", "Potwierdź adres e-mail")
}

func (s *authService) sendAuthToken(ctx context.Context, u user, purpose, path, subject string) error {
	now := s.now().UTC()
	var last string
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM auth_tokens WHERE user_id=? AND purpose=? ORDER BY created_at DESC LIMIT 1`, u.ID, purpose).Scan(&last)
	if err == nil {
		created, parseErr := time.Parse(time.RFC3339Nano, last)
		if parseErr != nil {
			return fmt.Errorf("parse auth token creation: %w", parseErr)
		}
		if now.Sub(created) < s.resendInterval {
			return errAuthRateLimited
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect auth token rate: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := s.random(raw); err != nil {
		return fmt.Errorf("generate auth token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if _, err := s.db.ExecContext(ctx, `INSERT INTO auth_tokens(user_id,purpose,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`, u.ID, purpose, hash[:], now.Add(s.tokenTTL).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("store auth token: %w", err)
	}
	link := strings.TrimRight(s.baseURL, "/") + path + url.QueryEscape(token)
	if err := s.mailer.Send(ctx, emailMessage{To: u.Email, Subject: subject, Text: subject + ": " + link}); err != nil {
		if _, deleteErr := s.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE token_hash=?`, hash[:]); deleteErr != nil {
			return fmt.Errorf("send authentication email: %v; remove unsent auth token: %w", err, deleteErr)
		}
		return fmt.Errorf("send authentication email: %w", err)
	}
	return nil
}

func (s *authService) resetPassword(ctx context.Context, token, password string) error {
	return s.consumeAuthToken(ctx, token, "password_reset", func(tx *sql.Tx, userID int64, now string) error {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash new password: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, string(hash), now, userID); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, now, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return nil
	})
}

func (s *authService) verifyEmail(ctx context.Context, token string) error {
	return s.consumeAuthToken(ctx, token, "email_verification", func(tx *sql.Tx, userID int64, now string) error {
		_, err := tx.ExecContext(ctx, `UPDATE users SET email_verified_at=COALESCE(email_verified_at,?),updated_at=? WHERE id=?`, now, now, userID)
		return err
	})
}

func (s *authService) consumeAuthToken(ctx context.Context, token, purpose string, apply func(*sql.Tx, int64, string) error) error {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return errInvalidAuthToken
	}
	hash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin auth token transaction: %w", err)
	}
	defer tx.Rollback()
	var id, userID int64
	var expires string
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,expires_at FROM auth_tokens WHERE token_hash=? AND purpose=? AND consumed_at IS NULL`, hash[:], purpose).Scan(&id, &userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return errInvalidAuthToken
	}
	if err != nil {
		return fmt.Errorf("load auth token: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return fmt.Errorf("parse auth token expiry: %w", err)
	}
	if !now.Before(expiresAt) {
		return errInvalidAuthToken
	}
	nowText := now.Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE auth_tokens SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, nowText, id)
	if err != nil {
		return fmt.Errorf("consume auth token: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errInvalidAuthToken
	}
	if err := apply(tx, userID, nowText); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_tokens SET consumed_at=? WHERE user_id=? AND purpose=? AND consumed_at IS NULL`, nowText, userID, purpose); err != nil {
		return fmt.Errorf("invalidate auth tokens: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth token: %w", err)
	}
	return nil
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
