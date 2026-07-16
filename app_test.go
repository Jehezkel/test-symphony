package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProfitabilityCSVExportFormattingFiltersAndAuthorization(t *testing.T) {
	db, handler := dashboardTestHandler(t)
	mustExec(t, db, `INSERT INTO users(id,email,display_name) VALUES (2,'other@example.com','Other')`)
	mustExec(t, db, `INSERT INTO products(id,user_id,sku,name,ean) VALUES (1,1,'SKU-ŻÓŁĆ','Żółta filiżanka','5901234567890'),(2,2,'SECRET','Inne konto','5900000000000')`)
	mustExec(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id) VALUES (1,1,'seller'),(2,2,'other-seller')`)
	mustExec(t, db, `INSERT INTO allegro_offers(id,integration_id,product_id,allegro_offer_id,external_sku,name,status) VALUES (1,1,1,'offer-1','SKU-ŻÓŁĆ','Żółta filiżanka','ACTIVE'),(2,2,2,'secret-offer','SECRET','Inne konto','ACTIVE')`)
	mustExec(t, db, `INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source) VALUES (1,1000,'PLN','1970-01-01','manual'),(2,1,'PLN','1970-01-01','manual')`)
	mustExec(t, db, `INSERT INTO allegro_orders(id,integration_id,allegro_order_id,status,currency,buyer_delivery_minor,seller_shipping_cost_minor,bought_at,source_updated_at) VALUES (1,1,'order-1','READY_FOR_PROCESSING','PLN',500,300,'2026-07-10T10:00:00Z','2026-07-10T10:00:00Z'),(2,1,'outside-range','READY_FOR_PROCESSING','PLN',0,0,'2026-06-10T10:00:00Z','2026-06-10T10:00:00Z'),(3,2,'other-account-order','READY_FOR_PROCESSING','PLN',0,0,'2026-07-10T10:00:00Z','2026-07-10T10:00:00Z')`)
	mustExec(t, db, `INSERT INTO order_items(id,order_id,offer_id,allegro_line_item_id,allegro_offer_id,name,quantity,unit_price_minor,currency,bought_at) VALUES (1,1,1,'line-1','offer-1','Żółta filiżanka',2,2500,'PLN','2026-07-10T10:00:00Z'),(2,2,1,'line-2','offer-1','Poza zakresem',1,9999,'PLN','2026-06-10T10:00:00Z'),(3,3,2,'line-3','secret-offer','Inne konto',1,9999,'PLN','2026-07-10T10:00:00Z')`)
	mustExec(t, db, `INSERT INTO allegro_fees(integration_id,order_id,offer_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at) VALUES (1,1,1,'fee-1','commission','Prowizja',-700,'PLN','2026-07-10')`)
	mustExec(t, db, `INSERT INTO order_adjustments(order_id,external_key,kind,value_minor,currency,status,source,occurred_at) VALUES (1,'adjustment-1','cost_correction',100,'PLN','finalized','manual','2026-07-10')`)

	response := request(t, handler, http.MethodGet, "/dashboard/export.csv?from=2026-07-01&to=2026-07-31&sort=revenue", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status = %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	body := response.Body.Bytes()
	if !bytes.HasPrefix(body, []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("CSV has no UTF-8 BOM")
	}
	reader := csv.NewReader(bytes.NewReader(body[3:]))
	reader.Comma = ';'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want header and one authorized, filtered row: %#v", len(records), records)
	}
	if strings.Join(records[0], "|") != strings.Join(profitabilityCSVHeader, "|") {
		t.Fatalf("header = %#v", records[0])
	}
	want := []string{"order-1", "offer-1", "SKU-ŻÓŁĆ", "5901234567890", "2026-07-10", "2", "55,00", "20,00", "7,00", "3,00", "1,00", "31,00", "24,00", "43,64", "PLN", "complete", "final"}
	if strings.Join(records[1], "|") != strings.Join(want, "|") {
		t.Fatalf("row = %#v, want %#v", records[1], want)
	}
	if strings.Contains(string(body), "outside-range") || strings.Contains(string(body), "other-account") || strings.Contains(string(body), "SECRET") {
		t.Fatalf("export leaked filtered or unauthorized data: %q", body)
	}
}

func TestProfitabilityCSVExportRejectsInvalidRange(t *testing.T) {
	_, handler := dashboardTestHandler(t)
	response := request(t, handler, http.MethodGet, "/dashboard/export.csv?from=bad&to=2026-07-31", nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid range status = %d", response.Code)
	}
}

func TestDashboardRendersKPIsLossesAndOfferDetail(t *testing.T) {
	db, handler := dashboardTestHandler(t)
	mustExec(t, db, `INSERT INTO products(id,user_id,sku,name) VALUES (1,1,'SKU-1','Drogi produkt'),(2,1,'SKU-2','Bez kosztu')`)
	mustExec(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id) VALUES (1,1,'seller')`)
	mustExec(t, db, `INSERT INTO allegro_offers(id,integration_id,product_id,allegro_offer_id,external_sku,name,status) VALUES (1,1,1,'offer-1','SKU-1','Drogi produkt','ACTIVE'),(2,1,2,'offer-2','SKU-2','Bez kosztu','ACTIVE')`)
	mustExec(t, db, `INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source) VALUES (1,10000,'PLN','1970-01-01','manual')`)
	mustExec(t, db, `INSERT INTO allegro_orders(id,integration_id,allegro_order_id,status,currency,buyer_delivery_minor,seller_shipping_cost_minor,bought_at,source_updated_at) VALUES (1,1,'order-1','READY_FOR_PROCESSING','PLN',1000,500,'2026-07-10T10:00:00Z','2026-07-10T10:00:00Z'),(2,1,'order-2','READY_FOR_PROCESSING','PLN',0,0,'2026-07-11T10:00:00Z','2026-07-11T10:00:00Z')`)
	mustExec(t, db, `INSERT INTO order_items(id,order_id,offer_id,allegro_line_item_id,allegro_offer_id,name,quantity,unit_price_minor,currency,bought_at) VALUES (1,1,1,'line-1','offer-1','Drogi produkt',1,10000,'PLN','2026-07-10T10:00:00Z'),(2,2,2,'line-2','offer-2','Bez kosztu',2,2500,'PLN','2026-07-11T10:00:00Z')`)
	mustExec(t, db, `INSERT INTO allegro_fees(integration_id,order_id,offer_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at) VALUES (1,1,1,'fee-1','commission','Prowizja',-1500,'PLN','2026-07-10'),(1,2,2,'fee-2','commission','Prowizja',-500,'PLN','2026-07-11')`)

	response := request(t, handler, http.MethodGet, "/dashboard?from=2026-07-01&to=2026-07-31", nil)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d: %s", response.Code, body)
	}
	for _, want := range []string{"160.00 PLN", "100.00 PLN", "20.00 PLN", "5.00 PLN", "35.00 PLN", "Wynik szacunkowy", "Nierentowna", "Brak kosztu zakupu"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}

	response = request(t, handler, http.MethodGet, "/dashboard/offers/1?from=2026-07-01&to=2026-07-31", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "order-1") || !strings.Contains(response.Body.String(), "Koszt produktów") {
		t.Fatalf("offer detail = %d %q", response.Code, response.Body.String())
	}
}

func TestDashboardEmptyAndInvalidRangeStates(t *testing.T) {
	_, handler := dashboardTestHandler(t)
	response := request(t, handler, http.MethodGet, "/dashboard?from=2026-07-01&to=2026-07-31", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Brak sprzedaży w tym okresie") {
		t.Fatalf("empty dashboard = %d %q", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet, "/dashboard/results?from=bad&to=2026-07-31", nil)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "alert-error") {
		t.Fatalf("invalid dashboard = %d %q", response.Code, response.Body.String())
	}
}

func dashboardTestHandler(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, newApp(newProductStore(db))
}

func TestProductCRUD(t *testing.T) {
	handler := testHandler(t)

	response := request(t, handler, http.MethodPost, "/products", url.Values{"name": {"Coffee"}, "price": {"12.50"}, "ean": {"5901234123457"}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Coffee") {
		t.Fatalf("create response = %d %q", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodGet, "/", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "5901234123457") {
		t.Fatalf("index response = %d %q", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodPut, "/products/1", url.Values{"name": {"Tea"}, "price": {"8.00"}, "ean": {"123"}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Tea") {
		t.Fatalf("update response = %d %q", response.Code, response.Body.String())
	}

	response = request(t, handler, http.MethodDelete, "/products/1", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("delete response = %d", response.Code)
	}
}

func TestProductValidationAndHealth(t *testing.T) {
	handler := testHandler(t)
	response := request(t, handler, http.MethodPost, "/products", url.Values{"name": {"Coffee"}, "price": {"nope"}, "ean": {"123"}})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid product response = %d", response.Code)
	}
	response = request(t, handler, http.MethodGet, "/health", nil)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health response = %d %q", response.Code, response.Body.String())
	}
}

func TestIndexDoesNotIncludeGreetingForDaniel(t *testing.T) {
	handler := testHandler(t)
	response := request(t, handler, http.MethodGet, "/", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("index response status = %d", response.Code)
	}
	for _, text := range []string{
		"Danielu, serdeczne pozdrowienia!",
		"Niech każdy poranek dobry rytm Ci nada",
		"Trzymaj się ciepło!",
	} {
		if strings.Contains(response.Body.String(), text) {
			t.Errorf("index response contains obsolete greeting %q", text)
		}
	}
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newApp(newProductStore(db))
}

func request(t *testing.T, handler http.Handler, method, target string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if values == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(values.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if values != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
