package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAllegroSynchronizationIsPaginatedAndIdempotent(t *testing.T) {
	offerCalls := 0
	s, _, _ := testAllegro(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sale/offers":
			offerCalls++
			if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("publication.status") != "ACTIVE" {
				t.Errorf("offer query = %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"offers":[{"id":"offer-1","name":"Coffee","status":"ACTIVE","external":{"id":"SKU-1"},"sellingMode":{"price":{"amount":"12.50","currency":"PLN"}},"updatedAt":"2026-07-16T10:00:00Z"}],"totalCount":1}`)
		case "/order/checkout-forms":
			io.WriteString(w, `{"checkoutForms":[{"id":"order-1","status":"READY_FOR_PROCESSING","updatedAt":"2026-07-16T11:00:00Z","fulfillment":{"status":"NEW"},"delivery":{"cost":{"amount":"8.99","currency":"PLN"}},"payment":{"paidAmount":{"amount":"33.99","currency":"PLN"}},"summary":{"totalToPay":{"amount":"33.99","currency":"PLN"}},"lineItems":[{"id":"line-1","name":"Coffee","offer":{"id":"offer-1","external":{"id":"SKU-1"}},"quantity":2,"price":{"amount":"12.50","currency":"PLN"},"boughtAt":"2026-07-16T10:30:00Z","discounts":[]}]}],"totalCount":1}`)
		case "/billing/billing-entries":
			io.WriteString(w, `{"billingEntries":[{"id":"fee-1","type":{"id":"SUC","name":"Commission"},"value":{"amount":"-2.50","currency":"PLN"},"occurredAt":"2026-07-16T11:30:00Z","order":{"id":"order-1"},"offer":{"id":"offer-1"}}],"totalCount":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	if err := s.save(context.Background(), 1, "seller", tokenPayload{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := s.synchronize(context.Background(), 1, "manual"); err != nil {
			t.Fatal(err)
		}
	}
	for table, want := range map[string]int{"allegro_offers": 1, "allegro_orders": 1, "order_items": 1, "allegro_fees": 1, "allegro_sync_runs": 2} {
		var got int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Errorf("%s count = %d, %v; want %d", table, got, err, want)
		}
	}
	if offerCalls != 2 {
		t.Fatalf("offer calls = %d", offerCalls)
	}
	status := s.latestSync(context.Background(), 1)
	if status.Status != "success" || status.Phase != "complete" || status.Processed != 3 {
		t.Fatalf("sync status = %+v", status)
	}
}

func TestAllegroClientRetriesRateLimitsAndServerErrors(t *testing.T) {
	calls := 0
	s, _, _ := testAllegro(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader([]int{http.StatusTooManyRequests, http.StatusBadGateway}[calls-1])
			return
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	if err := s.save(context.Background(), 1, "seller", tokenPayload{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := s.apiJSON(context.Background(), 1, "/resource", &response); err != nil || !response.OK || calls != 3 {
		t.Fatalf("response = %+v, calls = %d, err = %v", response, calls, err)
	}
}

func TestAllegroPageFailureRollsBackEveryRecord(t *testing.T) {
	s, _, _ := testAllegro(t, http.NotFoundHandler())
	if _, err := s.db.Exec(`INSERT INTO allegro_integrations(id,user_id,allegro_account_id) VALUES(1,1,'seller')`); err != nil {
		t.Fatal(err)
	}
	price := struct {
		Price allegroMoney `json:"price"`
	}{Price: allegroMoney{Amount: "1.00", Currency: "PLN"}}
	items := []allegroOffer{{ID: "offer-1", Name: "Valid", Status: "ACTIVE", SellingMode: price}, {Name: "Invalid", Status: "ACTIVE", SellingMode: price}}
	if err := s.storeOffers(context.Background(), 1, items); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("store error = %v", err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM allegro_offers").Scan(&count); err != nil || count != 0 {
		t.Fatalf("offer count = %d, %v", count, err)
	}
}
