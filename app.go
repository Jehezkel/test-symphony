package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type app struct {
	products          *productStore
	allegro           *allegroService
	profits           *profitabilityEngine
	auth              *authService
	logger            *log.Logger
	enforceOnboarding bool
}

type authFormData struct {
	Mode         string
	Email        string
	Next         string
	GeneralError string
	Errors       map[string]string
	Token        string
	Success      string
}

func authTitle(mode string) string {
	switch mode {
	case "register":
		return "Rejestracja"
	case "forgot-password":
		return "Reset hasła"
	case "reset-password":
		return "Ustaw nowe hasło"
	case "verify-email":
		return "Weryfikacja e-mail"
	}
	return "Logowanie"
}

func authButton(mode string) string {
	if mode == "register" {
		return "Utwórz konto"
	}
	if mode == "forgot-password" {
		return "Wyślij link"
	}
	if mode == "reset-password" {
		return "Zmień hasło"
	}
	return "Zaloguj się"
}

func passwordAutocomplete(mode string) string {
	if mode == "register" {
		return "new-password"
	}
	return "current-password"
}

func passwordMinLength(mode string) string {
	if mode == "register" {
		return "12"
	}
	return "1"
}

func newApp(products *productStore, services ...*allegroService) http.Handler {
	a := &app{products: products, profits: newProfitabilityEngine(products.db), logger: log.Default()}
	if len(services) > 0 {
		a.allegro = services[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("POST /products", a.createProduct)
	mux.HandleFunc("GET /products/{id}/edit", a.editProduct)
	mux.HandleFunc("PUT /products/{id}", a.updateProduct)
	mux.HandleFunc("DELETE /products/{id}", a.deleteProduct)
	mux.HandleFunc("GET /costs", a.costs)
	mux.HandleFunc("POST /costs/{id}", a.updateCost)
	mux.HandleFunc("GET /costs/template.csv", a.costTemplate)
	mux.HandleFunc("POST /costs/import", a.importCosts)
	mux.HandleFunc("GET /integration/allegro", a.allegroStatus)
	mux.HandleFunc("GET /oauth/allegro/start", a.allegroStart)
	mux.HandleFunc("GET /oauth/allegro/callback", a.allegroCallback)
	mux.HandleFunc("POST /integration/allegro/disconnect", a.allegroDisconnect)
	mux.HandleFunc("POST /integration/allegro/sync", a.allegroSync)
	mux.HandleFunc("GET /dashboard", a.dashboard)
	mux.HandleFunc("GET /dashboard/results", a.dashboardResults)
	mux.HandleFunc("GET /dashboard/export.csv", a.dashboardExport)
	mux.HandleFunc("GET /dashboard/offers/{id}", a.dashboardOffer)
	return withUser(mux, user{ID: 1, Email: "local@localhost", DisplayName: "Local user"})
}

func withUser(next http.Handler, u user) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, u)))
	})
}

func requestUserID(r *http.Request) int64 {
	u, ok := currentUser(r.Context())
	if !ok {
		return 0
	}
	return u.ID
}

func newAuthenticatedApp(products *productStore, allegro *allegroService, auth *authService) http.Handler {
	a := &app{products: products, profits: newProfitabilityEngine(products.db), allegro: allegro, auth: auth, logger: log.Default(), enforceOnboarding: true}
	public := http.NewServeMux()
	public.HandleFunc("GET /health", a.health)
	public.HandleFunc("GET /{$}", a.landing)
	public.HandleFunc("GET /login", a.loginPage)
	public.HandleFunc("POST /login", a.login)
	public.HandleFunc("GET /register", a.registerPage)
	public.HandleFunc("POST /register", a.register)
	public.HandleFunc("GET /forgot-password", a.forgotPasswordPage)
	public.HandleFunc("POST /forgot-password", a.forgotPassword)
	public.HandleFunc("GET /reset-password", a.resetPasswordPage)
	public.HandleFunc("POST /reset-password", a.resetPassword)
	public.HandleFunc("GET /verify-email", a.verifyEmail)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /products", a.index)
	protected.HandleFunc("GET /onboarding", a.onboarding)
	protected.HandleFunc("POST /logout", a.logout)
	protected.HandleFunc("GET /account", a.accountSettings)
	protected.HandleFunc("POST /account/password", a.changePassword)
	protected.HandleFunc("POST /account/sessions/revoke", a.revokeOtherSessions)
	protected.HandleFunc("POST /account/delete", a.deleteAccount)
	protected.HandleFunc("POST /verify-email/resend", a.resendVerification)
	protected.HandleFunc("POST /products", a.createProduct)
	protected.HandleFunc("GET /products/{id}/edit", a.editProduct)
	protected.HandleFunc("PUT /products/{id}", a.updateProduct)
	protected.HandleFunc("DELETE /products/{id}", a.deleteProduct)
	protected.HandleFunc("GET /costs", a.costs)
	protected.HandleFunc("POST /costs/{id}", a.updateCost)
	protected.HandleFunc("GET /costs/template.csv", a.costTemplate)
	protected.HandleFunc("POST /costs/import", a.importCosts)
	protected.HandleFunc("GET /integration/allegro", a.allegroStatus)
	protected.HandleFunc("GET /oauth/allegro/start", a.allegroStart)
	protected.HandleFunc("GET /oauth/allegro/callback", a.allegroCallback)
	protected.HandleFunc("POST /integration/allegro/disconnect", a.allegroDisconnect)
	protected.HandleFunc("POST /integration/allegro/sync", a.allegroSync)
	protected.HandleFunc("GET /dashboard", a.dashboard)
	protected.HandleFunc("GET /dashboard/results", a.dashboardResults)
	protected.HandleFunc("GET /dashboard/export.csv", a.dashboardExport)
	protected.HandleFunc("GET /dashboard/offers/{id}", a.dashboardOffer)

	public.Handle("/", a.requireUser(protected))
	return public
}

func (a *app) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, loginURL(r), http.StatusSeeOther)
			return
		}
		u, err := a.auth.userForToken(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, a.auth.cookie("", time.Unix(1, 0)))
			http.Redirect(w, r, loginURL(r), http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey{}, u)
		ctx = context.WithValue(ctx, csrfContextKey{}, csrfToken(cookie.Value))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loginURL(r *http.Request) string {
	if r.Method != http.MethodGet || r.URL.RequestURI() == "/" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(r.URL.RequestURI())
}

func (a *app) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	_, err = a.auth.userForToken(r.Context(), cookie.Value)
	return err == nil
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.authenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	data := authFormData{Mode: "login", Next: safeNext(r.URL.Query().Get("next"))}
	if r.URL.Query().Get("registered") == "1" {
		data.Success = "Konto zostało utworzone. Możesz się teraz zalogować."
	}
	a.renderAuth(w, r, data, http.StatusOK)
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderAuth(w, r, authFormData{Mode: "login", GeneralError: "Nie udało się odczytać formularza."}, http.StatusBadRequest)
		return
	}
	data := authFormData{Mode: "login", Email: strings.TrimSpace(r.FormValue("email")), Next: safeNext(r.FormValue("next"))}
	u, err := a.auth.authenticate(r.Context(), data.Email, r.FormValue("password"))
	if err != nil {
		data.GeneralError = "Nieprawidłowy adres e-mail lub hasło."
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	token, expires, err := a.auth.createSession(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.auth.cookie(token, expires))
	destination := data.Next
	if destination == "" {
		destination = "/dashboard"
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (a *app) registerPage(w http.ResponseWriter, r *http.Request) {
	if a.authenticated(r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.renderAuth(w, r, authFormData{Mode: "register"}, http.StatusOK)
}

func (a *app) register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderAuth(w, r, authFormData{Mode: "register", GeneralError: "Nie udało się odczytać formularza."}, http.StatusBadRequest)
		return
	}
	data := authFormData{Mode: "register", Email: strings.TrimSpace(r.FormValue("email"))}
	data.Errors = validateRegistration(data.Email, r.FormValue("password"), r.FormValue("password_confirmation"))
	if len(data.Errors) > 0 {
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	u, err := a.auth.register(r.Context(), data.Email, r.FormValue("password"))
	if errors.Is(err, errEmailUnavailable) {
		data.GeneralError = "Nie można utworzyć konta z podanymi danymi."
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		data.GeneralError = "Nie udało się utworzyć konta. Spróbuj ponownie za chwilę."
		a.renderAuth(w, r, data, http.StatusInternalServerError)
		return
	}
	if err := a.auth.sendVerification(r.Context(), u.ID); err != nil && !errors.Is(err, errAuthRateLimited) {
		// Account creation succeeded. Verification can be requested again after login,
		// so an email delivery failure must not turn this into an ambiguous result.
	}
	http.Redirect(w, r, "/login?registered=1", http.StatusSeeOther)
}

func (a *app) forgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	a.renderAuth(w, r, authFormData{Mode: "forgot-password"}, http.StatusOK)
}

func (a *app) forgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderAuth(w, r, authFormData{Mode: "forgot-password", GeneralError: "Nie udało się odczytać formularza."}, http.StatusBadRequest)
		return
	}
	if err := a.auth.sendPasswordReset(r.Context(), r.FormValue("email")); err != nil && !errors.Is(err, errAuthRateLimited) {
		http.Error(w, "request password reset", http.StatusInternalServerError)
		return
	}
	a.renderAuth(w, r, authFormData{Mode: "forgot-password", Success: "Jeśli konto istnieje, wysłaliśmy wiadomość z dalszymi instrukcjami."}, http.StatusOK)
}

func (a *app) resetPasswordPage(w http.ResponseWriter, r *http.Request) {
	a.renderAuth(w, r, authFormData{Mode: "reset-password", Token: r.URL.Query().Get("token")}, http.StatusOK)
}

func (a *app) resetPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	data := authFormData{Mode: "reset-password", Token: r.FormValue("token")}
	data.Errors = validateRegistration("reset@example.com", r.FormValue("password"), r.FormValue("password_confirmation"))
	delete(data.Errors, "email")
	if len(data.Errors) > 0 {
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	if err := a.auth.resetPassword(r.Context(), data.Token, r.FormValue("password")); errors.Is(err, errInvalidAuthToken) {
		data.GeneralError = "Link jest nieprawidłowy, wygasł lub został już użyty."
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		http.Error(w, "reset password", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) verifyEmail(w http.ResponseWriter, r *http.Request) {
	data := authFormData{Mode: "verify-email"}
	if err := a.auth.verifyEmail(r.Context(), r.URL.Query().Get("token")); errors.Is(err, errInvalidAuthToken) {
		data.GeneralError = "Link jest nieprawidłowy, wygasł lub został już użyty."
		a.renderAuth(w, r, data, http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		http.Error(w, "verify email", http.StatusInternalServerError)
		return
	}
	data.Success = "Adres e-mail został potwierdzony."
	a.renderAuth(w, r, data, http.StatusOK)
}

func (a *app) resendVerification(w http.ResponseWriter, r *http.Request) {
	cookie, cookieErr := r.Cookie(sessionCookieName)
	if cookieErr != nil || !validCSRF(cookie.Value, r.FormValue("csrf_token")) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	err := a.auth.sendVerification(r.Context(), requestUserID(r))
	if errors.Is(err, errAuthRateLimited) {
		http.Error(w, "Spróbuj ponownie za chwilę.", http.StatusTooManyRequests)
		return
	}
	if err != nil {
		a.logger.Printf("resend verification failed: stage=%s", smtpErrorStage(err))
		http.Error(w, "resend verification", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func safeNext(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "/login" || parsed.Path == "/register" {
		return ""
	}
	return raw
}

func (a *app) renderAuth(w http.ResponseWriter, r *http.Request, data authFormData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := authPage(data).Render(r.Context(), w); err != nil {
		return
	}
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !validCSRF(cookie.Value, r.FormValue("csrf_token")) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if cookie != nil {
		if err := a.auth.revoke(r.Context(), cookie.Value); err != nil {
			http.Error(w, "revoke session", http.StatusInternalServerError)
			return
		}
	}
	http.SetCookie(w, a.auth.cookie("", time.Unix(1, 0)))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) costs(w http.ResponseWriter, r *http.Request) {
	items, err := a.products.listCosts(r.Context(), requestUserID(r))
	if err != nil {
		http.Error(w, "load product costs", http.StatusInternalServerError)
		return
	}
	if err := costsPage(items, nil).Render(r.Context(), w); err != nil {
		http.Error(w, "render costs", http.StatusInternalServerError)
	}
}

func (a *app) updateCost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	cost, err := parseMinorUnits(r.FormValue("unit_purchase_cost"))
	if err != nil || cost < 0 || strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))) != "PLN" {
		http.Error(w, "cost must be a non-negative PLN amount with at most two decimal places", http.StatusUnprocessableEntity)
		return
	}
	if err := a.products.setCost(r.Context(), requestUserID(r), id, cost, "PLN", "manual"); errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "save product cost", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/costs", http.StatusSeeOther)
}

func (a *app) costTemplate(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="koszty-produktow.csv"`)
	_, _ = w.Write([]byte("sku,ean,offer_id,unit_purchase_cost,currency\nSKU-001,,,12.34,PLN\n"))
}

func (a *app) importCosts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "invalid CSV upload", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "CSV file is required", http.StatusUnprocessableEntity)
		return
	}
	defer file.Close()
	report, err := a.products.importCosts(r.Context(), requestUserID(r), file)
	if err != nil {
		http.Error(w, "import product costs", http.StatusBadRequest)
		return
	}
	items, err := a.products.listCosts(r.Context(), requestUserID(r))
	if err != nil {
		http.Error(w, "load product costs", http.StatusInternalServerError)
		return
	}
	if err := costsPage(items, &report).Render(r.Context(), w); err != nil {
		http.Error(w, "render import report", http.StatusInternalServerError)
	}
}

func (a *app) allegroSync(w http.ResponseWriter, r *http.Request) {
	message := "Synchronizacja zakończona pomyślnie."
	if a.allegro == nil {
		message = "Integracja Allegro nie jest skonfigurowana."
	} else if err := a.allegro.synchronize(r.Context(), requestUserID(r), "manual"); err != nil {
		message = "Synchronizacja nie powiodła się. Można ją bezpiecznie ponowić."
	}
	http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *app) allegroStatus(w http.ResponseWriter, r *http.Request) {
	status := integrationStatus{Message: r.URL.Query().Get("message")}
	if a.allegro != nil {
		status = a.allegro.status(r.Context(), requestUserID(r), status.Message)
	}
	if err := allegroPage(status).Render(r.Context(), w); err != nil {
		http.Error(w, "render integration", http.StatusInternalServerError)
	}
}

func (a *app) allegroStart(w http.ResponseWriter, r *http.Request) {
	if a.allegro == nil {
		http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape("Integracja Allegro nie jest skonfigurowana."), http.StatusSeeOther)
		return
	}
	location, err := a.allegro.begin(r.Context(), requestUserID(r))
	if err != nil {
		http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape("Nie udało się rozpocząć połączenia z Allegro."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, location, http.StatusFound)
}

func (a *app) allegroCallback(w http.ResponseWriter, r *http.Request) {
	message := "Konto Allegro zostało połączone."
	if a.allegro == nil {
		message = "Integracja Allegro nie jest skonfigurowana."
	} else if err := a.allegro.consumeState(r.Context(), r.URL.Query().Get("state"), requestUserID(r)); err != nil {
		message = "Sesja łączenia wygasła lub jest nieprawidłowa. Spróbuj ponownie."
	} else if r.URL.Query().Get("error") != "" {
		message = "Połączenie zostało odrzucone lub anulowane w Allegro."
	} else if token, err := a.allegro.exchange(r.Context(), r.URL.Query().Get("code")); err != nil {
		message = "Allegro nie zaakceptowało autoryzacji. Spróbuj ponownie później."
	} else if accountID, err := a.allegro.accountID(r.Context(), token.AccessToken); err != nil {
		message = "Nie udało się pobrać danych konta z Allegro."
	} else if err := a.allegro.save(r.Context(), requestUserID(r), accountID, token); err != nil {
		message = "Nie udało się bezpiecznie zapisać połączenia."
	}
	http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *app) allegroDisconnect(w http.ResponseWriter, r *http.Request) {
	if a.auth != nil && !validRequestCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	message := "Konto Allegro zostało rozłączone."
	if a.allegro != nil {
		if err := a.allegro.disconnect(r.Context(), requestUserID(r)); err != nil {
			message = "Nie udało się rozłączyć konta Allegro."
		}
	}
	http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/products" {
		http.NotFound(w, r)
		return
	}
	products, err := a.products.list(r.Context(), requestUserID(r))
	if err != nil {
		http.Error(w, "load products", http.StatusInternalServerError)
		return
	}
	if err := page(products).Render(r.Context(), w); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func (a *app) landing(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := landingPage().Render(r.Context(), w); err != nil {
		http.Error(w, "render landing page", http.StatusInternalServerError)
	}
}

func (a *app) createProduct(w http.ResponseWriter, r *http.Request) {
	name, price, ean, ok := productFields(w, r)
	if !ok {
		return
	}
	item, err := a.products.create(r.Context(), requestUserID(r), name, price, ean)
	if err != nil {
		http.Error(w, "create product", http.StatusInternalServerError)
		return
	}
	if err := productRow(item).Render(r.Context(), w); err != nil {
		http.Error(w, "render product", http.StatusInternalServerError)
	}
}

func (a *app) editProduct(w http.ResponseWriter, r *http.Request) {
	item, ok := a.findProduct(w, r)
	if !ok {
		return
	}
	if err := productEditRow(item).Render(r.Context(), w); err != nil {
		http.Error(w, "render product", http.StatusInternalServerError)
	}
}

func (a *app) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}
	name, price, ean, ok := productFields(w, r)
	if !ok {
		return
	}
	item, err := a.products.update(r.Context(), requestUserID(r), id, name, price, ean)
	if errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "update product", http.StatusInternalServerError)
		return
	}
	if err := productRow(item).Render(r.Context(), w); err != nil {
		http.Error(w, "render product", http.StatusInternalServerError)
	}
}

func (a *app) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}
	if err := a.products.delete(r.Context(), requestUserID(r), id); errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "delete product", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *app) findProduct(w http.ResponseWriter, r *http.Request) (product, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return product{}, false
	}
	item, err := a.products.get(r.Context(), requestUserID(r), id)
	if errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return product{}, false
	}
	if err != nil {
		http.Error(w, "load product", http.StatusInternalServerError)
		return product{}, false
	}
	return item, true
}

func productFields(w http.ResponseWriter, r *http.Request) (string, int64, string, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return "", 0, "", false
	}
	name := strings.TrimSpace(r.FormValue("name"))
	price := strings.TrimSpace(r.FormValue("price"))
	ean := strings.TrimSpace(r.FormValue("ean"))
	if name == "" || price == "" || ean == "" {
		http.Error(w, "name, price and EAN are required", http.StatusUnprocessableEntity)
		return "", 0, "", false
	}
	priceMinor, err := parseMinorUnits(price)
	if err != nil || priceMinor < 0 {
		http.Error(w, "price must be a non-negative amount with at most two decimal places", http.StatusUnprocessableEntity)
		return "", 0, "", false
	}
	return name, priceMinor, ean, true
}
