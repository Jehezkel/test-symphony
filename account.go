package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

const deleteAccountConfirmation = "USUŃ KONTO"

type accountData struct {
	Email            string
	EmailVerified    bool
	AllegroConnected bool
	Success          string
	PasswordErrors   map[string]string
	DeletionError    string
}

func (a *app) accountSettings(w http.ResponseWriter, r *http.Request) {
	a.loadAndRenderAccount(w, r, accountData{Success: r.URL.Query().Get("success")}, http.StatusOK)
}

func (a *app) renderAccount(w http.ResponseWriter, r *http.Request, data accountData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = accountPage(data).Render(r.Context(), w)
}

func validRequestCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	return err == nil && validCSRF(cookie.Value, r.FormValue("csrf_token"))
}

func (a *app) changePassword(w http.ResponseWriter, r *http.Request) {
	if !validRequestCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	data := accountData{PasswordErrors: validateRegistration("account@example.com", r.FormValue("password"), r.FormValue("password_confirmation"))}
	delete(data.PasswordErrors, "email")
	if r.FormValue("current_password") == "" {
		data.PasswordErrors["current_password"] = "Podaj aktualne hasło."
	}
	if len(data.PasswordErrors) > 0 {
		a.loadAndRenderAccount(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	if err := a.auth.changePassword(r.Context(), requestUserID(r), r.FormValue("current_password"), r.FormValue("password")); errors.Is(err, errInvalidCredentials) {
		data.PasswordErrors["current_password"] = "Aktualne hasło jest nieprawidłowe."
		a.loadAndRenderAccount(w, r, data, http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		http.Error(w, "change password", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account?success=Hasło+zostało+zmienione.", http.StatusSeeOther)
}

func (a *app) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	if !validRequestCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	cookie, _ := r.Cookie(sessionCookieName)
	if err := a.auth.revokeOtherSessions(r.Context(), requestUserID(r), cookie.Value); err != nil {
		http.Error(w, "revoke sessions", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account?success=Pozostałe+sesje+zostały+wylogowane.", http.StatusSeeOther)
}

func (a *app) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if !validRequestCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if strings.TrimSpace(r.FormValue("confirmation")) != deleteAccountConfirmation {
		a.loadAndRenderAccount(w, r, accountData{DeletionError: "Wpisz dokładnie „" + deleteAccountConfirmation + "”."}, http.StatusUnprocessableEntity)
		return
	}
	if err := a.auth.deleteAccount(r.Context(), requestUserID(r), r.FormValue("password")); errors.Is(err, errInvalidCredentials) {
		a.loadAndRenderAccount(w, r, accountData{DeletionError: "Hasło jest nieprawidłowe."}, http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		http.Error(w, "delete account", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.auth.cookie("", time.Unix(1, 0)))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) loadAndRenderAccount(w http.ResponseWriter, r *http.Request, data accountData, status int) {
	var verified sql.NullString
	if err := a.products.db.QueryRowContext(r.Context(), `SELECT email,email_verified_at FROM users WHERE id=?`, requestUserID(r)).Scan(&data.Email, &verified); err != nil {
		http.Error(w, "load account", http.StatusInternalServerError)
		return
	}
	data.EmailVerified = verified.Valid
	if a.allegro != nil {
		data.AllegroConnected = a.allegro.status(r.Context(), requestUserID(r), "").Connected
	}
	a.renderAccount(w, r, data, status)
}
