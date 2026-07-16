package main

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSessionCreateReadExpireAndRevoke(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	auth := newAuthService(db, time.Hour, true)
	auth.now = func() time.Time { return now }
	if err := auth.ensureUser(t.Context(), "owner@example.com", "Owner", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	u, err := auth.authenticate(t.Context(), "OWNER@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "wrong"); err != errInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}

	token, expires, err := auth.createSession(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || !expires.Equal(now.Add(time.Hour)) {
		t.Fatalf("session token/expires = %q %v", token, expires)
	}
	var stored []byte
	if err := db.QueryRow(`SELECT token_hash FROM user_sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte(token))
	if string(stored) != string(wantHash[:]) || string(stored) == token {
		t.Fatal("database did not contain only the session token hash")
	}
	if got, err := auth.userForToken(t.Context(), token); err != nil || got.ID != u.ID {
		t.Fatalf("read session = %#v, %v", got, err)
	}

	if err := auth.revoke(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.userForToken(t.Context(), token); err != errInvalidCredentials {
		t.Fatalf("revoked session error = %v", err)
	}

	token, _, err = auth.createSession(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if _, err := auth.userForToken(t.Context(), token); err != errInvalidCredentials {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestAuthenticationMiddlewareLoginCookieAndLogout(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, true)
	if err := auth.ensureUser(t.Context(), "owner@example.com", "Owner", "a strong password"); err != nil {
		t.Fatal(err)
	}
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)

	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/costs?from=test", nil))
	if protected.Code != http.StatusSeeOther || protected.Header().Get("Location") != "/login?next=%2Fcosts%3Ffrom%3Dtest" {
		t.Fatalf("anonymous response = %d %q", protected.Code, protected.Header().Get("Location"))
	}

	form := url.Values{"email": {"owner@example.com"}, "password": {"a strong password"}, "next": {"/costs"}}
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, req)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login response = %d %q", login.Code, login.Body.String())
	}
	if login.Header().Get("Location") != "/costs" {
		t.Fatalf("login redirect = %q", login.Header().Get("Location"))
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %#v", cookies)
	}

	authorized := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized response = %d", authorized.Code)
	}

	logout := httptest.NewRecorder()
	badLogout := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(cookies[0])
	handler.ServeHTTP(badLogout, req)
	if badLogout.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF = %d", badLogout.Code)
	}

	logoutForm := url.Values{"csrf_token": {csrfToken(cookies[0].Value)}}
	req = httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(logoutForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookies[0])
	handler.ServeHTTP(logout, req)
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("logout response = %d", logout.Code)
	}

	rejected := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])
	handler.ServeHTTP(rejected, req)
	if rejected.Code != http.StatusSeeOther {
		t.Fatalf("revoked cookie response = %d", rejected.Code)
	}
}

func TestRegistrationValidationDuplicateAndSession(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)

	invalid := url.Values{"email": {"bad"}, "password": {"short"}, "password_confirmation": {"different"}}
	response := postForm(handler, "/register", invalid)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "prawidłowy adres") || len(response.Result().Cookies()) != 0 {
		t.Fatalf("invalid registration = %d %q", response.Code, response.Body.String())
	}

	valid := url.Values{"email": {"new@example.com"}, "password": {"StrongPassword1"}, "password_confirmation": {"StrongPassword1"}}
	response = postForm(handler, "/register", valid)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" || len(response.Result().Cookies()) != 1 {
		t.Fatalf("registration = %d %q %#v", response.Code, response.Header().Get("Location"), response.Result().Cookies())
	}

	response = postForm(handler, "/register", valid)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Nie można utworzyć konta") || strings.Contains(response.Body.String(), "UNIQUE") {
		t.Fatalf("duplicate registration = %d %q", response.Code, response.Body.String())
	}
}

func TestLoginRejectsUnsafeNextAndAuthenticatedAuthPages(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	if err := auth.ensureUser(t.Context(), "owner@example.com", "Owner", "StrongPassword1"); err != nil {
		t.Fatal(err)
	}
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)
	response := postForm(handler, "/login", url.Values{"email": {"owner@example.com"}, "password": {"StrongPassword1"}, "next": {"https://evil.example"}})
	if response.Header().Get("Location") != "/dashboard" {
		t.Fatalf("unsafe redirect = %q", response.Header().Get("Location"))
	}
	cookie := response.Result().Cookies()[0]
	for _, path := range []string{"/login", "/register"} {
		page := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		handler.ServeHTTP(page, req)
		if page.Code != http.StatusSeeOther || page.Header().Get("Location") != "/dashboard" {
			t.Fatalf("authenticated %s = %d %q", path, page.Code, page.Header().Get("Location"))
		}
	}
}

func postForm(handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(response, req)
	return response
}
