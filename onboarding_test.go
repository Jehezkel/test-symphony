package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOnboardingProgressPersistsAndControlsDashboard(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	u, err := auth.register(t.Context(), "onboarding@example.com", "StrongPassword1")
	if err != nil {
		t.Fatal(err)
	}
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)
	cookie := sessionCookie(t, auth, u.ID)

	response := onboardingRequest(handler, cookie, "/dashboard")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/onboarding" {
		t.Fatalf("empty dashboard = %d %q", response.Code, response.Header().Get("Location"))
	}
	response = onboardingRequest(handler, cookie, "/onboarding")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Połącz konto Allegro") || !strings.Contains(response.Body.String(), "Następny krok") {
		t.Fatalf("initial onboarding = %d %q", response.Code, response.Body.String())
	}

	mustExecArgs(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id,access_token_ciphertext) VALUES(10,?,'seller',X'01')`, u.ID)
	mustExec(t, db, `INSERT INTO allegro_sync_runs(integration_id,trigger,status,started_at,finished_at) VALUES(10,'manual','success','2026-07-16T10:00:00Z','2026-07-16T10:01:00Z')`)
	mustExecArgs(t, db, `INSERT INTO products(id,user_id,sku,name) VALUES(10,?,'SKU-10','Produkt')`, u.ID)
	mustExec(t, db, `INSERT INTO allegro_offers(id,integration_id,product_id,allegro_offer_id,name,status) VALUES(10,10,10,'offer-10','Produkt','ACTIVE')`)
	mustExec(t, db, `INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source) VALUES(10,1200,'PLN','1970-01-01','manual')`)

	response = onboardingRequest(handler, cookie, "/onboarding")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/dashboard?onboarding=1") {
		t.Fatalf("ready onboarding = %d %q", response.Code, response.Body.String())
	}
	response = onboardingRequest(handler, cookie, "/dashboard")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("dashboard bypassed onboarding: %d", response.Code)
	}
	response = onboardingRequest(handler, cookie, "/dashboard?onboarding=1")
	if response.Code != http.StatusOK {
		t.Fatalf("first report = %d %q", response.Code, response.Body.String())
	}
	response = onboardingRequest(handler, cookie, "/onboarding")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/dashboard" {
		t.Fatalf("completed onboarding = %d %q", response.Code, response.Header().Get("Location"))
	}
	response = onboardingRequest(handler, cookie, "/dashboard")
	if response.Code != http.StatusOK {
		t.Fatalf("persisted dashboard access = %d", response.Code)
	}
}

func TestOnboardingShowsFailedSynchronization(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auth := newAuthService(db, time.Hour, false)
	u, _ := auth.register(t.Context(), "failed@example.com", "StrongPassword1")
	handler := newAuthenticatedApp(newProductStore(db), nil, auth)
	mustExecArgs(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id,access_token_ciphertext) VALUES(20,?,'seller',X'01')`, u.ID)
	mustExec(t, db, `INSERT INTO allegro_sync_runs(integration_id,trigger,status,error_message,started_at,finished_at) VALUES(20,'manual','failed','API timeout','2026-07-16T10:00:00Z','2026-07-16T10:01:00Z')`)

	response := onboardingRequest(handler, sessionCookie(t, auth, u.ID), "/onboarding")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Synchronizacja nie powiodła się") || !strings.Contains(response.Body.String(), "Spróbuj ponownie") {
		t.Fatalf("failed synchronization = %d %q", response.Code, response.Body.String())
	}
}

func onboardingRequest(handler http.Handler, cookie *http.Cookie, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(response, req)
	return response
}

func mustExecArgs(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
