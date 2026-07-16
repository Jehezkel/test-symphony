package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testAllegro(t *testing.T, api http.Handler) (*allegroService, http.Handler, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := allegroConfig{ClientID: "client", ClientSecret: "secret", RedirectURL: "http://app.test/oauth/allegro/callback", AuthorizeURL: server.URL + "/authorize", TokenURL: server.URL + "/token", APIURL: server.URL, EncryptionKey: []byte("01234567890123456789012345678901")}
	s := newAllegroService(db, c, server.Client())
	s.now = func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) }
	return s, newApp(newProductStore(db), s), server
}

func TestAllegroOAuthCallbackStoresEncryptedTokens(t *testing.T) {
	var authorization string
	s, handler, _ := testAllegro(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			authorization = r.Header.Get("Authorization")
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "valid-code" {
				t.Errorf("unexpected token form: %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/me" {
			if r.Header.Get("Authorization") != "Bearer access-secret" {
				t.Error("missing bearer token")
			}
			io.WriteString(w, `{"id":"seller-123"}`)
			return
		}
		http.NotFound(w, r)
	}))
	location, err := s.begin(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := url.Parse(location)
	response := request(t, handler, http.MethodGet, "/oauth/allegro/callback?code=valid-code&state="+url.QueryEscape(state.Query().Get("state")), nil)
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "Konto+Allegro") {
		t.Fatalf("callback = %d %s", response.Code, response.Header().Get("Location"))
	}
	if authorization != "Basic "+base64.StdEncoding.EncodeToString([]byte("client:secret")) {
		t.Error("token exchange did not use client authentication")
	}
	var access, refresh []byte
	if err := s.db.QueryRow(`SELECT access_token_ciphertext,refresh_token_ciphertext FROM allegro_integrations WHERE allegro_account_id='seller-123'`).Scan(&access, &refresh); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(access), "access-secret") || strings.Contains(string(refresh), "refresh-secret") {
		t.Fatal("token stored in plaintext")
	}
}

func TestAllegroOAuthRejectsInvalidAndReusedState(t *testing.T) {
	s, handler, _ := testAllegro(t, http.NotFoundHandler())
	response := request(t, handler, http.MethodGet, "/oauth/allegro/callback?code=x&state=invalid", nil)
	if !strings.Contains(response.Header().Get("Location"), "Sesja+") {
		t.Fatalf("invalid state location %q", response.Header().Get("Location"))
	}
	location, _ := s.begin(context.Background(), 1)
	u, _ := url.Parse(location)
	state := u.Query().Get("state")
	if err := s.consumeState(context.Background(), state, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.consumeState(context.Background(), state, 1); err != errInvalidOAuthState {
		t.Fatalf("reuse error = %v", err)
	}
}

func TestAllegroRefreshesExpiredToken(t *testing.T) {
	refreshes := 0
	s, _, _ := testAllegro(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes++
			r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
				t.Errorf("refresh form = %v", r.Form)
			}
			io.WriteString(w, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)
			return
		}
		http.NotFound(w, r)
	}))
	if err := s.save(context.Background(), 1, "seller", tokenPayload{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresIn: -1}); err != nil {
		t.Fatal(err)
	}
	token, err := s.accessToken(context.Background(), 1, false)
	if err != nil || token != "new-access" || refreshes != 1 {
		t.Fatalf("refresh = %q, %v, calls %d", token, err, refreshes)
	}
}

func TestAllegroAPIRetries401AndReturnsAPIErrors(t *testing.T) {
	apiCalls := 0
	s, _, _ := testAllegro(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"access_token":"fresh","refresh_token":"fresh-refresh","expires_in":3600}`)
			return
		}
		if r.URL.Path == "/resource" {
			apiCalls++
			if apiCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
			} else {
				w.WriteHeader(http.StatusBadGateway)
			}
			return
		}
		http.NotFound(w, r)
	}))
	if err := s.save(context.Background(), 1, "seller", tokenPayload{AccessToken: "current", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	resp, err := s.doAPI(context.Background(), 1, http.MethodGet, "/resource")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || apiCalls != 2 {
		t.Fatalf("API response = %d, calls %d", resp.StatusCode, apiCalls)
	}
}

func TestAllegroConfigurationValidation(t *testing.T) {
	t.Setenv("ALLEGRO_CLIENT_ID", "only-one")
	t.Setenv("ALLEGRO_CLIENT_SECRET", "")
	t.Setenv("ALLEGRO_REDIRECT_URL", "")
	t.Setenv("ALLEGRO_TOKEN_ENCRYPTION_KEY", "")
	if _, err := allegroConfigFromEnv(); err == nil {
		t.Fatal("incomplete configuration accepted")
	}
}
