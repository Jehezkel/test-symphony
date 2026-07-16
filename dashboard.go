package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type dashboardRange struct{ From, To string }

type dashboardKPI struct {
	Revenue, Purchase, Allegro, Other, Profit int64
	Margin                                    *int64
	Currency                                  string
	Estimated                                 bool
}

type dashboardOfferItem struct {
	ID                                        int64
	AllegroID, Name, SKU, Currency            string
	Sales, Orders                             int64
	Revenue, Purchase, Allegro, Other, Profit int64
	Margin                                    *int64
	MissingCost, Estimated                    bool
	Lines                                     []dashboardOrderLine
}

type dashboardOrderLine struct {
	OrderID                                   int64
	AllegroOrderID, BoughtAt, Completeness    string
	Quantity                                  int64
	Revenue, Purchase, Allegro, Other, Profit int64
	Estimated                                 bool
}

type dashboardData struct {
	Range  dashboardRange
	KPI    dashboardKPI
	Offers []dashboardOfferItem
	Sort   string
}

var errDashboardOfferNotFound = errors.New("dashboard offer not found")

func defaultDashboardRange(now time.Time) dashboardRange {
	to := now.UTC()
	return dashboardRange{From: to.AddDate(0, 0, -29).Format("2006-01-02"), To: to.Format("2006-01-02")}
}

func parseDashboardRange(r *http.Request, now time.Time) (dashboardRange, string, error) {
	rng := defaultDashboardRange(now)
	if r.URL.Query().Get("from") != "" {
		rng.From = r.URL.Query().Get("from")
	}
	if r.URL.Query().Get("to") != "" {
		rng.To = r.URL.Query().Get("to")
	}
	from, err := time.Parse("2006-01-02", rng.From)
	if err != nil {
		return rng, "", errors.New("nieprawidłowa data początkowa")
	}
	to, err := time.Parse("2006-01-02", rng.To)
	if err != nil || to.Before(from) {
		return rng, "", errors.New("nieprawidłowy zakres dat")
	}
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "margin", "revenue", "sales":
	default:
		sortBy = "profit"
	}
	return rng, sortBy, nil
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	if a.enforceOnboarding {
		progress, progressErr := a.loadOnboardingProgress(r.Context(), requestUserID(r))
		if progressErr != nil {
			http.Error(w, "load onboarding progress", http.StatusInternalServerError)
			return
		}
		if !progress.complete() {
			if r.URL.Query().Get("onboarding") != "1" || !progress.readyForReport() {
				http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
				return
			}
			if _, err := a.products.db.ExecContext(r.Context(), `INSERT OR IGNORE INTO onboarding_report_views(user_id) VALUES(?)`, requestUserID(r)); err != nil {
				http.Error(w, "save onboarding progress", http.StatusInternalServerError)
				return
			}
		}
	}
	rng, sortBy, err := parseDashboardRange(r, time.Now())
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = dashboardPage(dashboardData{Range: rng, Sort: sortBy}, err.Error()).Render(r.Context(), w)
		return
	}
	data, loadErr := a.loadDashboard(r.Context(), rng, sortBy)
	if loadErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = dashboardPage(dashboardData{Range: rng, Sort: sortBy}, "Nie udało się wczytać danych. Spróbuj ponownie.").Render(r.Context(), w)
		return
	}
	if err := dashboardPage(data, "").Render(r.Context(), w); err != nil {
		http.Error(w, "render dashboard", http.StatusInternalServerError)
	}
}

func (a *app) dashboardResults(w http.ResponseWriter, r *http.Request) {
	rng, sortBy, err := parseDashboardRange(r, time.Now())
	if err == nil {
		var data dashboardData
		data, err = a.loadDashboard(r.Context(), rng, sortBy)
		if err == nil {
			err = dashboardResults(data).Render(r.Context(), w)
			if err != nil {
				http.Error(w, "render dashboard", http.StatusInternalServerError)
			}
			return
		}
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = dashboardError("Nie udało się wczytać wyników. Sprawdź zakres dat i spróbuj ponownie.").Render(r.Context(), w)
}

func (a *app) dashboardOffer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	rng, _, rangeErr := parseDashboardRange(r, time.Now())
	if err != nil || rangeErr != nil {
		http.Error(w, "invalid offer or date range", http.StatusBadRequest)
		return
	}
	data, err := a.loadDashboard(r.Context(), rng, "profit")
	if err != nil {
		http.Error(w, "load offer profitability", http.StatusInternalServerError)
		return
	}
	for _, offer := range data.Offers {
		if offer.ID == id {
			_ = dashboardOfferDetail(offer, rng).Render(r.Context(), w)
			return
		}
	}
	http.NotFound(w, r)
}

func (a *app) loadDashboard(ctx context.Context, rng dashboardRange, sortBy string) (dashboardData, error) {
	data := dashboardData{Range: rng, Sort: sortBy, KPI: dashboardKPI{Currency: "PLN"}}
	to, _ := time.Parse("2006-01-02", rng.To)
	u, ok := currentUser(ctx)
	if !ok {
		return data, errors.New("authenticated user missing")
	}
	rows, err := a.products.db.QueryContext(ctx, `SELECT o.id,o.allegro_order_id,o.bought_at FROM allegro_orders o JOIN allegro_integrations i ON i.id=o.integration_id WHERE i.user_id=? AND o.bought_at>=? AND o.bought_at<? ORDER BY o.bought_at,o.id`, u.ID, rng.From, to.AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		return data, fmt.Errorf("load dashboard orders: %w", err)
	}
	type orderMeta struct {
		id                  int64
		allegroID, boughtAt string
	}
	var orders []orderMeta
	for rows.Next() {
		var o orderMeta
		if err := rows.Scan(&o.id, &o.allegroID, &o.boughtAt); err != nil {
			rows.Close()
			return data, err
		}
		orders = append(orders, o)
	}
	if err := rows.Close(); err != nil {
		return data, err
	}
	offers := map[int64]*dashboardOfferItem{}
	for _, order := range orders {
		result, err := a.profits.CalculateOrder(ctx, u.ID, order.id)
		if err != nil {
			return data, err
		}
		if result.Quality == "excluded_cancelled" {
			continue
		}
		if result.Currency != "PLN" {
			continue
		}
		data.KPI.Revenue += result.RevenueMinor
		data.KPI.Purchase += result.PurchaseCostMinor
		data.KPI.Allegro += result.AllegroCostMinor
		data.KPI.Other += result.ShippingCostMinor + result.AdjustmentCostMinor
		data.KPI.Profit += result.ProfitMinor
		if result.Completeness != "complete" || result.Quality != "final" {
			data.KPI.Estimated = true
		}
		for _, line := range result.Lines {
			if line.OfferID == 0 {
				continue
			}
			item := offers[line.OfferID]
			if item == nil {
				item = &dashboardOfferItem{ID: line.OfferID, Currency: result.Currency}
				err := a.products.db.QueryRowContext(ctx, `SELECT o.allegro_offer_id,o.name,COALESCE(o.external_sku,'') FROM allegro_offers o JOIN allegro_integrations i ON i.id=o.integration_id WHERE o.id=? AND i.user_id=?`, line.OfferID, u.ID).Scan(&item.AllegroID, &item.Name, &item.SKU)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return data, err
				}
				offers[line.OfferID] = item
			}
			var quantity int64
			if err := a.products.db.QueryRowContext(ctx, `SELECT quantity FROM order_items WHERE id=?`, line.ID).Scan(&quantity); err != nil {
				return data, err
			}
			other := line.ShippingCostMinor + line.CostCorrectionMinor
			item.Sales += quantity
			item.Orders++
			item.Revenue += line.RevenueMinor
			item.Purchase += line.PurchaseCostMinor
			item.Allegro += line.AllegroCostMinor
			item.Other += other
			item.Profit += line.ProfitMinor
			missing := result.Completeness == "missing_product_cost" || result.Completeness == "missing_multiple_costs"
			item.MissingCost = item.MissingCost || missing
			item.Estimated = item.Estimated || result.Completeness != "complete" || result.Quality != "final"
			item.Lines = append(item.Lines, dashboardOrderLine{OrderID: order.id, AllegroOrderID: order.allegroID, BoughtAt: order.boughtAt, Quantity: quantity, Revenue: line.RevenueMinor, Purchase: line.PurchaseCostMinor, Allegro: line.AllegroCostMinor, Other: other, Profit: line.ProfitMinor, Completeness: result.Completeness, Estimated: result.Quality != "final"})
		}
	}
	if data.KPI.Revenue != 0 {
		m := roundedRatio(data.KPI.Profit*10000, data.KPI.Revenue)
		data.KPI.Margin = &m
	}
	for _, item := range offers {
		if item.Revenue != 0 {
			m := roundedRatio(item.Profit*10000, item.Revenue)
			item.Margin = &m
		}
		data.Offers = append(data.Offers, *item)
	}
	sort.SliceStable(data.Offers, func(i, j int) bool {
		a, b := data.Offers[i], data.Offers[j]
		switch sortBy {
		case "margin":
			return marginSort(a.Margin, b.Margin, a.Profit, b.Profit)
		case "revenue":
			return a.Revenue > b.Revenue
		case "sales":
			return a.Sales > b.Sales
		default:
			return a.Profit < b.Profit
		}
	})
	return data, nil
}

func marginSort(a, b *int64, ap, bp int64) bool {
	if a == nil {
		return b != nil
	}
	if b == nil {
		return false
	}
	if *a == *b {
		return ap < bp
	}
	return *a < *b
}

func dashboardSortURL(sortBy string, rng dashboardRange) string {
	return "/dashboard/results?from=" + rng.From + "&to=" + rng.To + "&sort=" + sortBy
}
func offerDetailURL(id int64, rng dashboardRange) string {
	return "/dashboard/offers/" + strconv.FormatInt(id, 10) + "?from=" + rng.From + "&to=" + rng.To
}
func formatMoney(v int64, currency string) string { return formatMinorUnits(v) + " " + currency }
func formatMargin(v *int64) string {
	if v == nil {
		return "N/D"
	}
	sign := ""
	n := *v
	if n < 0 {
		sign = "-"
		n = -n
	}
	return fmt.Sprintf("%s%d.%02d%%", sign, n/100, n%100)
}
func profitClass(v int64) string {
	if v < 0 {
		return "text-error"
	}
	return ""
}
func shortDate(v string) string {
	if len(v) >= 10 {
		return v[:10]
	}
	return strings.TrimSpace(v)
}
