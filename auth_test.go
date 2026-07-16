package main

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUserDataIsolationForReadAndModification(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	first, err := auth.register(t.Context(), "first@example.com", "StrongPassword1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.register(t.Context(), "second@example.com", "StrongPassword2")
	if err != nil {
		t.Fatal(err)
	}
	store := newProductStore(db)
	firstProduct, err := store.create(t.Context(), first.ID, "First private product", 1000, "FIRST-EAN")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.create(t.Context(), second.ID, "Second private product", 2000, "SECOND-EAN"); err != nil {
		t.Fatal(err)
	}
	handler := newAuthenticatedApp(store, nil, auth)
	firstCookie := sessionCookie(t, auth, first.ID)
	secondCookie := sessionCookie(t, auth, second.ID)

	firstIndex := authenticatedRequest(handler, firstCookie, http.MethodGet, "/products", nil)
	if firstIndex.Code != http.StatusOK || !strings.Contains(firstIndex.Body.String(), "First private product") || strings.Contains(firstIndex.Body.String(), "Second private product") {
		t.Fatalf("first index leaked data: %d %q", firstIndex.Code, firstIndex.Body.String())
	}
	secondIndex := authenticatedRequest(handler, secondCookie, http.MethodGet, "/products", nil)
	if secondIndex.Code != http.StatusOK || !strings.Contains(secondIndex.Body.String(), "Second private product") || strings.Contains(secondIndex.Body.String(), "First private product") {
		t.Fatalf("second index leaked data: %d %q", secondIndex.Code, secondIndex.Body.String())
	}

	foreignPath := "/products/" + strconv.FormatInt(firstProduct.ID, 10)
	for _, attempt := range []struct {
		method string
		path   string
		form   url.Values
	}{
		{http.MethodGet, foreignPath + "/edit", nil},
		{http.MethodPut, foreignPath, url.Values{"name": {"Stolen"}, "price": {"1.00"}, "ean": {"STOLEN"}}},
		{http.MethodDelete, foreignPath, nil},
		{http.MethodPost, "/costs/" + strconv.FormatInt(firstProduct.ID, 10), url.Values{"unit_purchase_cost": {"1.00"}, "currency": {"PLN"}}},
	} {
		response := authenticatedRequest(handler, secondCookie, attempt.method, attempt.path, attempt.form)
		if response.Code != http.StatusNotFound {
			t.Errorf("foreign %s %s = %d, want 404", attempt.method, attempt.path, response.Code)
		}
	}
	if item, err := store.get(t.Context(), first.ID, firstProduct.ID); err != nil || item.Name != "First private product" {
		t.Fatalf("foreign mutation changed owner data: %#v, %v", item, err)
	}
}

func TestDashboardExportOnlyContainsAuthenticatedUsersOrders(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	first, _ := auth.register(t.Context(), "csv-first@example.com", "StrongPassword1")
	second, _ := auth.register(t.Context(), "csv-second@example.com", "StrongPassword2")
	for _, seed := range []struct {
		userID int64
		prefix string
	}{
		{first.ID, "FIRST-PRIVATE"},
		{second.ID, "SECOND-PRIVATE"},
	} {
		result, err := db.Exec(`INSERT INTO allegro_integrations(user_id,allegro_account_id) VALUES(?,?)`, seed.userID, seed.prefix+"-ACCOUNT")
		if err != nil {
			t.Fatal(err)
		}
		integrationID, _ := result.LastInsertId()
		result, err = db.Exec(`INSERT INTO products(user_id,sku,name) VALUES(?,?,?)`, seed.userID, seed.prefix+"-SKU", seed.prefix+" product")
		if err != nil {
			t.Fatal(err)
		}
		productID, _ := result.LastInsertId()
		result, err = db.Exec(`INSERT INTO allegro_offers(integration_id,product_id,allegro_offer_id,name,status) VALUES(?,?,?,?,?)`, integrationID, productID, seed.prefix+"-OFFER", seed.prefix+" offer", "ACTIVE")
		if err != nil {
			t.Fatal(err)
		}
		offerID, _ := result.LastInsertId()
		result, err = db.Exec(`INSERT INTO allegro_orders(integration_id,allegro_order_id,status,currency,buyer_delivery_minor,seller_shipping_cost_minor,bought_at,source_updated_at) VALUES(?,?,?,?,?,?,?,?)`, integrationID, seed.prefix+"-ORDER", "READY_FOR_PROCESSING", "PLN", 0, 0, "2026-07-10T00:00:00Z", "2026-07-10T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		orderID, _ := result.LastInsertId()
		if _, err := db.Exec(`INSERT INTO order_items(order_id,offer_id,allegro_line_item_id,allegro_offer_id,name,quantity,unit_price_minor,currency,bought_at) VALUES(?,?,?,?,?,?,?,?,?)`, orderID, offerID, seed.prefix+"-LINE", seed.prefix+"-OFFER", seed.prefix+" item", 1, 1000, "PLN", "2026-07-10T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)
	response := authenticatedRequest(handler, sessionCookie(t, auth, second.ID), http.MethodGet, "/dashboard/export.csv?from=2026-07-01&to=2026-07-31", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "SECOND-PRIVATE-ORDER") || strings.Contains(response.Body.String(), "FIRST-PRIVATE-ORDER") {
		t.Fatalf("CSV export leaked data: %d %q", response.Code, response.Body.String())
	}
}

func sessionCookie(t *testing.T, auth *authService, userID int64) *http.Cookie {
	t.Helper()
	token, expires, err := auth.createSession(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return auth.cookie(token, expires)
}

func authenticatedRequest(handler http.Handler, cookie *http.Cookie, method, target string, form url.Values) *httptest.ResponseRecorder {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

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
	req = httptest.NewRequest(http.MethodGet, "/products", nil)
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
	req = httptest.NewRequest(http.MethodGet, "/products", nil)
	req.AddCookie(cookies[0])
	handler.ServeHTTP(rejected, req)
	if rejected.Code != http.StatusSeeOther {
		t.Fatalf("revoked cookie response = %d", rejected.Code)
	}
}

func TestLandingIsPublicAndCTAsAreFunctional(t *testing.T) {
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

	landing := httptest.NewRecorder()
	handler.ServeHTTP(landing, httptest.NewRequest(http.MethodGet, "/", nil))
	body := landing.Body.String()
	if landing.Code != http.StatusOK {
		t.Fatalf("landing status = %d: %s", landing.Code, body)
	}
	for _, want := range []string{"Sprawdź, ile naprawdę zarabiasz na Allegro", `href="/register"`, `href="/login"`, `id="korzysci"`, `id="jak-to-dziala"`, `id="cennik"`, `id="faq"`, `name="description"`} {
		if !strings.Contains(body, want) {
			t.Errorf("landing does not contain %q", want)
		}
	}

	for _, path := range []string{"/login", "/register"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("anonymous CTA %s = %d", path, response.Code)
		}
	}

	cookie := sessionCookie(t, auth, 1)
	cta := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(cta, req)
	if cta.Code != http.StatusSeeOther || cta.Header().Get("Location") != "/dashboard" {
		t.Fatalf("authenticated CTA = %d %q", cta.Code, cta.Header().Get("Location"))
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
