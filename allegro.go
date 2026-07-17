package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const allegroScope = "allegro:api:sale:offers:read allegro:api:orders:read allegro:api:billing:read"

var (
	errAllegroNotConfigured = errors.New("Allegro integration is not configured")
	errInvalidOAuthState    = errors.New("invalid or expired OAuth state")
	errAllegroDisconnected  = errors.New("Allegro account is not connected")
)

type allegroConfig struct {
	ClientID, ClientSecret, RedirectURL string
	AuthorizeURL, TokenURL, APIURL      string
	EncryptionKey                       []byte
}

type allegroEndpoints struct {
	AuthorizeURL string
	TokenURL     string
	APIURL       string
}

func allegroEnvironment(environment string) (string, allegroEndpoints, error) {
	switch environment {
	case "", "sandbox":
		return "SANDBOX", allegroEndpoints{
			AuthorizeURL: "https://allegro.pl.allegrosandbox.pl/auth/oauth/authorize",
			TokenURL:     "https://allegro.pl.allegrosandbox.pl/auth/oauth/token",
			APIURL:       "https://api.allegro.pl.allegrosandbox.pl",
		}, nil
	case "production":
		return "PRODUCTION", allegroEndpoints{
			AuthorizeURL: "https://allegro.pl/auth/oauth/authorize",
			TokenURL:     "https://allegro.pl/auth/oauth/token",
			APIURL:       "https://api.allegro.pl",
		}, nil
	default:
		return "", allegroEndpoints{}, fmt.Errorf("ALLEGRO_ENVIRONMENT must be production or sandbox, got %q", environment)
	}
}

func allegroConfigFromEnv() (allegroConfig, error) {
	prefix, endpoints, err := allegroEnvironment(strings.ToLower(strings.TrimSpace(os.Getenv("ALLEGRO_ENVIRONMENT"))))
	if err != nil {
		return allegroConfig{}, err
	}
	variable := func(suffix string) string { return "ALLEGRO_" + prefix + "_" + suffix }
	c := allegroConfig{
		ClientID:     os.Getenv(variable("CLIENT_ID")),
		ClientSecret: os.Getenv(variable("CLIENT_SECRET")),
		RedirectURL:  os.Getenv(variable("REDIRECT_URL")),
		AuthorizeURL: endpoints.AuthorizeURL,
		TokenURL:     endpoints.TokenURL,
		APIURL:       endpoints.APIURL,
	}
	keyText := os.Getenv("ALLEGRO_TOKEN_ENCRYPTION_KEY")
	if c.ClientID == "" && c.ClientSecret == "" && c.RedirectURL == "" && keyText == "" {
		return c, nil
	}
	if c.ClientID == "" || c.ClientSecret == "" || c.RedirectURL == "" || keyText == "" {
		return c, fmt.Errorf("%s, %s, %s and ALLEGRO_TOKEN_ENCRYPTION_KEY must be set together", variable("CLIENT_ID"), variable("CLIENT_SECRET"), variable("REDIRECT_URL"))
	}
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return c, errors.New("ALLEGRO_TOKEN_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	c.EncryptionKey = key
	for _, raw := range []string{c.RedirectURL, c.AuthorizeURL, c.TokenURL, c.APIURL} {
		if u, err := url.Parse(raw); err != nil || u.Scheme == "" || u.Host == "" {
			return c, errors.New("Allegro URLs must be absolute")
		}
	}
	return c, nil
}

func (c allegroConfig) configured() bool { return c.ClientID != "" && len(c.EncryptionKey) == 32 }

type tokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}
type allegroIntegration struct {
	ID                        int64
	AccountID                 string
	AccessToken, RefreshToken string
	ExpiresAt                 time.Time
}
type integrationStatus struct {
	Configured, Connected bool
	AccountID, Message    string
	Sync                  syncStatus
}

type allegroService struct {
	db         *sql.DB
	config     allegroConfig
	httpClient *http.Client
	now        func() time.Time
	syncMu     sync.Mutex
}

func newAllegroService(db *sql.DB, config allegroConfig, client *http.Client) *allegroService {
	if client == nil {
		client = http.DefaultClient
	}
	return &allegroService{db: db, config: config, httpClient: client, now: time.Now}
}

func (s *allegroService) begin(ctx context.Context, userID int64) (string, error) {
	if !s.config.configured() {
		return "", errAllegroNotConfigured
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(state))
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM allegro_oauth_states WHERE expires_at <= ?`, now.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("clean OAuth states: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO allegro_oauth_states(state_hash,user_id,expires_at) VALUES (?,?,?)`, hash[:], userID, now.Add(10*time.Minute).Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("save OAuth state: %w", err)
	}
	u, _ := url.Parse(s.config.AuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", s.config.ClientID)
	q.Set("redirect_uri", s.config.RedirectURL)
	q.Set("scope", allegroScope)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *allegroService) consumeState(ctx context.Context, state string, userID int64) error {
	if state == "" {
		return errInvalidOAuthState
	}
	hash := sha256.Sum256([]byte(state))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expires string
	if err := tx.QueryRowContext(ctx, `SELECT expires_at FROM allegro_oauth_states WHERE state_hash=? AND user_id=?`, hash[:], userID).Scan(&expires); errors.Is(err, sql.ErrNoRows) {
		return errInvalidOAuthState
	} else if err != nil {
		return fmt.Errorf("load OAuth state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM allegro_oauth_states WHERE state_hash=?`, hash[:]); err != nil {
		return fmt.Errorf("delete OAuth state: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !s.now().Before(expiresAt) {
		return errInvalidOAuthState
	}
	return tx.Commit()
}

func (s *allegroService) exchange(ctx context.Context, code string) (tokenPayload, error) {
	if code == "" {
		return tokenPayload{}, errors.New("authorization code is missing")
	}
	return s.requestToken(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.config.RedirectURL}})
}

func (s *allegroService) requestToken(ctx context.Context, form url.Values) (tokenPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenPayload{}, err
	}
	req.SetBasicAuth(s.config.ClientID, s.config.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return tokenPayload{}, fmt.Errorf("Allegro authorization is temporarily unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenPayload{}, errors.New("could not read Allegro authorization response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenPayload{}, fmt.Errorf("Allegro rejected authorization (status %d)", resp.StatusCode)
	}
	var token tokenPayload
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return tokenPayload{}, errors.New("Allegro returned an invalid authorization response")
	}
	return token, nil
}

func (s *allegroService) accountID(ctx context.Context, accessToken string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.config.APIURL, "/")+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.allegro.public.v1+json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Allegro API is temporarily unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Allegro API returned status %d", resp.StatusCode)
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&me); err != nil || me.ID == "" {
		return "", errors.New("Allegro returned an invalid account response")
	}
	return me.ID, nil
}

func (s *allegroService) encrypt(plain string) ([]byte, error) {
	block, err := aes.NewCipher(s.config.EncryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}
func (s *allegroService) decrypt(data []byte) (string, error) {
	block, err := aes.NewCipher(s.config.EncryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted token")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("decrypt token")
	}
	return string(plain), nil
}

func (s *allegroService) save(ctx context.Context, userID int64, accountID string, token tokenPayload) error {
	access, err := s.encrypt(token.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := s.encrypt(token.RefreshToken)
	if err != nil {
		return err
	}
	expires := s.now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO allegro_integrations(user_id,allegro_account_id,access_token_ciphertext,refresh_token_ciphertext,token_expires_at) VALUES (?,?,?,?,?) ON CONFLICT(user_id,allegro_account_id) DO UPDATE SET access_token_ciphertext=excluded.access_token_ciphertext,refresh_token_ciphertext=excluded.refresh_token_ciphertext,token_expires_at=excluded.token_expires_at,updated_at=CURRENT_TIMESTAMP`, userID, accountID, access, refresh, expires)
	if err != nil {
		return fmt.Errorf("save Allegro integration: %w", err)
	}
	return nil
}

func (s *allegroService) load(ctx context.Context, userID int64) (allegroIntegration, error) {
	var i allegroIntegration
	var access, refresh []byte
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT id,allegro_account_id,access_token_ciphertext,refresh_token_ciphertext,token_expires_at FROM allegro_integrations WHERE user_id=? AND access_token_ciphertext IS NOT NULL ORDER BY updated_at DESC LIMIT 1`, userID).Scan(&i.ID, &i.AccountID, &access, &refresh, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return i, errAllegroDisconnected
	}
	if err != nil {
		return i, err
	}
	i.AccessToken, err = s.decrypt(access)
	if err != nil {
		return i, err
	}
	i.RefreshToken, err = s.decrypt(refresh)
	if err != nil {
		return i, err
	}
	i.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	return i, err
}

func (s *allegroService) accessToken(ctx context.Context, userID int64, force bool) (string, error) {
	i, err := s.load(ctx, userID)
	if err != nil {
		return "", err
	}
	if !force && s.now().Add(time.Minute).Before(i.ExpiresAt) {
		return i.AccessToken, nil
	}
	token, err := s.requestToken(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {i.RefreshToken}})
	if err != nil {
		return "", fmt.Errorf("refresh Allegro authorization: %w", err)
	}
	if err := s.save(ctx, userID, i.AccountID, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (s *allegroService) doAPI(ctx context.Context, userID int64, method, path string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.accessToken(ctx, userID, attempt == 1)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.config.APIURL, "/")+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.allegro.public.v1+json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Allegro API is temporarily unavailable: %w", err)
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt == 1 {
			return resp, nil
		}
		resp.Body.Close()
	}
	return nil, errors.New("Allegro API authorization failed")
}

func (s *allegroService) status(ctx context.Context, userID int64, message string) integrationStatus {
	status := integrationStatus{Configured: s.config.configured(), Message: message}
	if !status.Configured {
		return status
	}
	i, err := s.load(ctx, userID)
	if err == nil {
		status.Connected = true
		status.AccountID = i.AccountID
		status.Sync = s.latestSync(ctx, userID)
	}
	return status
}
func (s *allegroService) disconnect(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM allegro_integrations WHERE user_id=?`, userID)
	return err
}
