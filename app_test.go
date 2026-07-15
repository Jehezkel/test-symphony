package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProductCRUD(t *testing.T) {
	handler := newApp(newProductStore())

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
	handler := newApp(newProductStore())
	response := request(t, handler, http.MethodPost, "/products", url.Values{"name": {"Coffee"}, "price": {"nope"}, "ean": {"123"}})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid product response = %d", response.Code)
	}
	response = request(t, handler, http.MethodGet, "/health", nil)
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("health response = %d %q", response.Code, response.Body.String())
	}
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
