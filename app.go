package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type app struct {
	products *productStore
	allegro  *allegroService
}

func newApp(products *productStore, services ...*allegroService) http.Handler {
	a := &app{products: products}
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
	return mux
}

func (a *app) costs(w http.ResponseWriter, r *http.Request) {
	items, err := a.products.listCosts(r.Context())
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
	if err := a.products.setCost(r.Context(), id, cost, "PLN", "manual"); errors.Is(err, errProductNotFound) {
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
	report, err := a.products.importCosts(r.Context(), file)
	if err != nil {
		http.Error(w, "import product costs", http.StatusBadRequest)
		return
	}
	items, err := a.products.listCosts(r.Context())
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
	} else if err := a.allegro.synchronize(r.Context(), 1, "manual"); err != nil {
		message = "Synchronizacja nie powiodła się. Można ją bezpiecznie ponowić."
	}
	http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *app) allegroStatus(w http.ResponseWriter, r *http.Request) {
	status := integrationStatus{Message: r.URL.Query().Get("message")}
	if a.allegro != nil {
		status = a.allegro.status(r.Context(), 1, status.Message)
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
	location, err := a.allegro.begin(r.Context(), 1)
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
	} else if err := a.allegro.consumeState(r.Context(), r.URL.Query().Get("state"), 1); err != nil {
		message = "Sesja łączenia wygasła lub jest nieprawidłowa. Spróbuj ponownie."
	} else if r.URL.Query().Get("error") != "" {
		message = "Połączenie zostało odrzucone lub anulowane w Allegro."
	} else if token, err := a.allegro.exchange(r.Context(), r.URL.Query().Get("code")); err != nil {
		message = "Allegro nie zaakceptowało autoryzacji. Spróbuj ponownie później."
	} else if accountID, err := a.allegro.accountID(r.Context(), token.AccessToken); err != nil {
		message = "Nie udało się pobrać danych konta z Allegro."
	} else if err := a.allegro.save(r.Context(), 1, accountID, token); err != nil {
		message = "Nie udało się bezpiecznie zapisać połączenia."
	}
	http.Redirect(w, r, "/integration/allegro?message="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *app) allegroDisconnect(w http.ResponseWriter, r *http.Request) {
	message := "Konto Allegro zostało rozłączone."
	if a.allegro != nil {
		if err := a.allegro.disconnect(r.Context(), 1); err != nil {
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
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	products, err := a.products.list()
	if err != nil {
		http.Error(w, "load products", http.StatusInternalServerError)
		return
	}
	if err := page(products).Render(r.Context(), w); err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
	}
}

func (a *app) createProduct(w http.ResponseWriter, r *http.Request) {
	name, price, ean, ok := productFields(w, r)
	if !ok {
		return
	}
	item, err := a.products.create(name, price, ean)
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
	item, err := a.products.update(id, name, price, ean)
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
	if err := a.products.delete(id); errors.Is(err, errProductNotFound) {
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
	item, err := a.products.get(id)
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
