package main

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func accountTestApp(t *testing.T) (*authService, *app, http.Handler, user, *http.Cookie) {
	t.Helper()
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, true)
	if err := auth.ensureUser(t.Context(), "owner@example.com", "Owner", "CurrentPassword1"); err != nil {
		t.Fatal(err)
	}
	u, err := auth.authenticate(t.Context(), "owner@example.com", "CurrentPassword1")
	if err != nil {
		t.Fatal(err)
	}
	application := &app{products: newProductStore(db), profits: newProfitabilityEngine(db), auth: auth}
	handler := newAuthenticatedApp(application.products, nil, auth)
	return auth, application, handler, u, sessionCookie(t, auth, u.ID)
}

func accountExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestChangePasswordRequiresCurrentPassword(t *testing.T) {
	auth, _, handler, u, cookie := accountTestApp(t)
	form := url.Values{
		"csrf_token":            {csrfToken(cookie.Value)},
		"current_password":      {"wrong"},
		"password":              {"NewPassword123"},
		"password_confirmation": {"NewPassword123"},
	}
	response := authenticatedRequest(handler, cookie, http.MethodPost, "/account/password", form)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Aktualne hasło jest nieprawidłowe") {
		t.Fatalf("wrong current password response = %d %q", response.Code, response.Body.String())
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "CurrentPassword1"); err != nil {
		t.Fatalf("old password changed after rejection: %v", err)
	}

	form.Set("current_password", "CurrentPassword1")
	response = authenticatedRequest(handler, cookie, http.MethodPost, "/account/password", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("password change response = %d %q", response.Code, response.Body.String())
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "CurrentPassword1"); err != errInvalidCredentials {
		t.Fatalf("old password error = %v", err)
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "NewPassword123"); err != nil {
		t.Fatalf("new password error = %v", err)
	}
}

func TestRevokeOtherSessionsKeepsCurrentSession(t *testing.T) {
	auth, _, handler, u, current := accountTestApp(t)
	other := sessionCookie(t, auth, u.ID)
	form := url.Values{"csrf_token": {csrfToken(current.Value)}}
	response := authenticatedRequest(handler, current, http.MethodPost, "/account/sessions/revoke", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("revoke sessions response = %d", response.Code)
	}
	if _, err := auth.userForToken(t.Context(), current.Value); err != nil {
		t.Fatalf("current session revoked: %v", err)
	}
	if _, err := auth.userForToken(t.Context(), other.Value); err != errInvalidCredentials {
		t.Fatalf("other session error = %v", err)
	}
}

func TestDeleteAccountRequiresConfirmationAndRemovesDependentData(t *testing.T) {
	auth, application, handler, u, cookie := accountTestApp(t)
	db := application.products.db
	accountExec(t, db, `UPDATE users SET email_verified_at='2026-07-17T00:00:00Z' WHERE id=?`, u.ID)
	accountExec(t, db, `INSERT INTO auth_tokens(user_id,purpose,token_hash,expires_at,created_at) VALUES(?, 'password_reset', X'01', '2099-01-01T00:00:00Z', '2026-07-17T00:00:00Z')`, u.ID)
	accountExec(t, db, `INSERT INTO allegro_oauth_states(state_hash,user_id,expires_at) VALUES(X'02',?, '2099-01-01T00:00:00Z')`, u.ID)
	accountExec(t, db, `INSERT INTO products(user_id,sku,name) VALUES(?,'SKU','Product')`, u.ID)
	accountExec(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id,access_token_ciphertext,refresh_token_ciphertext) VALUES(1,?,'seller',X'03',X'04')`, u.ID)
	mustExec(t, db, `INSERT INTO allegro_sync_runs(integration_id,trigger,status,started_at) VALUES(1,'scheduled','running','2026-07-17T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO allegro_sync_checkpoints(integration_id,resource,cursor) VALUES(1,'offers','next')`)

	form := url.Values{"csrf_token": {csrfToken(cookie.Value)}, "password": {"CurrentPassword1"}, "confirmation": {"usuń konto"}}
	response := authenticatedRequest(handler, cookie, http.MethodPost, "/account/delete", form)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ambiguous confirmation response = %d", response.Code)
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "CurrentPassword1"); err != nil {
		t.Fatalf("account deleted without exact confirmation: %v", err)
	}

	form.Set("confirmation", deleteAccountConfirmation)
	response = authenticatedRequest(handler, cookie, http.MethodPost, "/account/delete", form)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("delete response = %d %q", response.Code, response.Header().Get("Location"))
	}
	if _, err := auth.authenticate(t.Context(), u.Email, "CurrentPassword1"); err != errInvalidCredentials {
		t.Fatalf("deleted login error = %v", err)
	}
	if _, err := auth.userForToken(t.Context(), cookie.Value); err != errInvalidCredentials {
		t.Fatalf("deleted session error = %v", err)
	}
	for _, table := range []string{"users", "user_sessions", "auth_tokens", "allegro_oauth_states", "products", "allegro_integrations", "allegro_sync_runs", "allegro_sync_checkpoints"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s count = %d, err = %v", table, count, err)
		}
	}
}

func TestAccountSettingsIsProtectedAndShowsStatuses(t *testing.T) {
	_, _, handler, _, cookie := accountTestApp(t)
	response := authenticatedRequest(handler, cookie, http.MethodGet, "/account", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("settings response = %d", response.Code)
	}
	for _, want := range []string{"owner@example.com", "Niepotwierdzony", "Niepołączono", "USUŃ KONTO"} {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("settings missing %q", want)
		}
	}
}
