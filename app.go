package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type app struct{ products *productStore }

func newApp(products *productStore) http.Handler {
	a := &app{products: products}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("POST /products", a.createProduct)
	mux.HandleFunc("GET /products/{id}/edit", a.editProduct)
	mux.HandleFunc("PUT /products/{id}", a.updateProduct)
	mux.HandleFunc("DELETE /products/{id}", a.deleteProduct)
	return mux
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if err := page(a.products.list()).Render(r.Context(), w); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func (a *app) createProduct(w http.ResponseWriter, r *http.Request) {
	name, price, ean, ok := productFields(w, r)
	if !ok {
		return
	}
	item := a.products.create(name, price, ean)
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
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}
	name, price, ean, ok := productFields(w, r)
	if !ok {
		return
	}
	item, err := a.products.update(id, name, price, ean)
	if errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return
	}
	if err := productRow(item).Render(r.Context(), w); err != nil {
		http.Error(w, "render product", http.StatusInternalServerError)
	}
}

func (a *app) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return
	}
	if err := a.products.delete(id); errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *app) findProduct(w http.ResponseWriter, r *http.Request) (product, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid product id", http.StatusBadRequest)
		return product{}, false
	}
	item, err := a.products.get(id)
	if errors.Is(err, errProductNotFound) {
		http.NotFound(w, r)
		return product{}, false
	}
	return item, true
}

func productFields(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return "", "", "", false
	}
	name := strings.TrimSpace(r.FormValue("name"))
	price := strings.TrimSpace(r.FormValue("price"))
	ean := strings.TrimSpace(r.FormValue("ean"))
	if name == "" || price == "" || ean == "" {
		http.Error(w, "name, price and EAN are required", http.StatusUnprocessableEntity)
		return "", "", "", false
	}
	if _, err := strconv.ParseFloat(price, 64); err != nil {
		http.Error(w, "price must be a number", http.StatusUnprocessableEntity)
		return "", "", "", false
	}
	return name, price, ean, true
}
