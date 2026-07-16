package main

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var profitabilityCSVHeader = []string{
	"ID zamówienia", "ID oferty", "SKU", "EAN", "Data sprzedaży", "Ilość",
	"Przychód", "Koszt zakupu", "Opłaty Allegro", "Koszt wysyłki",
	"Korekty kosztów", "Koszty razem", "Zysk", "Marża", "Waluta",
	"Kompletność danych", "Jakość wyniku",
}

func (a *app) dashboardExport(w http.ResponseWriter, r *http.Request) {
	rng, _, err := parseDashboardRange(r, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rentownosc-%s-%s.csv"`, rng.From, rng.To))
	// UTF-8 BOM makes Excel on Windows recognize Polish characters without an
	// import wizard. Semicolons and decimal commas match the Polish locale.
	if _, err := w.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
		return
	}
	writer := csv.NewWriter(w)
	writer.Comma = ';'
	writer.UseCRLF = true
	if err := writer.Write(profitabilityCSVHeader); err != nil {
		return
	}

	to, _ := time.Parse("2006-01-02", rng.To)
	upperBound := to.AddDate(0, 0, 1).Format("2006-01-02")
	cursorBoughtAt, cursorID := rng.From, int64(0)
	for {
		var orderID int64
		var allegroOrderID, boughtAt string
		err := a.products.db.QueryRowContext(r.Context(), `SELECT o.id,o.allegro_order_id,o.bought_at
			FROM allegro_orders o
			JOIN allegro_integrations i ON i.id=o.integration_id
			WHERE i.user_id=? AND o.bought_at>=? AND o.bought_at<?
			AND (o.bought_at>? OR (o.bought_at=? AND o.id>?))
			ORDER BY o.bought_at,o.id LIMIT 1`, currentUserID(r), rng.From, upperBound, cursorBoughtAt, cursorBoughtAt, cursorID).
			Scan(&orderID, &allegroOrderID, &boughtAt)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return
		}
		cursorBoughtAt, cursorID = boughtAt, orderID
		result, err := a.profits.CalculateOrder(r.Context(), orderID)
		if err != nil {
			return
		}
		if result.Quality == "excluded_cancelled" || result.Currency != "PLN" {
			continue
		}
		for _, line := range result.Lines {
			var offerID, sku, ean string
			var quantity int64
			err := a.products.db.QueryRowContext(r.Context(), `SELECT oi.quantity,COALESCE(ao.allegro_offer_id,''),COALESCE(ao.external_sku,''),COALESCE(p.ean,'')
				FROM order_items oi
				LEFT JOIN allegro_offers ao ON ao.id=oi.offer_id
				LEFT JOIN products p ON p.id=ao.product_id
				WHERE oi.id=?`, line.ID).Scan(&quantity, &offerID, &sku, &ean)
			if err != nil {
				return
			}
			margin := "N/D"
			if line.RevenueMinor != 0 && result.Completeness == "complete" {
				margin = formatCSVPercent(roundedRatio(line.ProfitMinor*10000, line.RevenueMinor))
			}
			costTotal := line.PurchaseCostMinor + line.AllegroCostMinor + line.ShippingCostMinor + line.CostCorrectionMinor
			record := []string{
				safeCSVText(allegroOrderID), safeCSVText(offerID), safeCSVText(sku), safeCSVText(ean), shortDate(boughtAt),
				strconv.FormatInt(quantity, 10), formatCSVAmount(line.RevenueMinor), formatCSVAmount(line.PurchaseCostMinor),
				formatCSVAmount(line.AllegroCostMinor), formatCSVAmount(line.ShippingCostMinor), formatCSVAmount(line.CostCorrectionMinor),
				formatCSVAmount(costTotal), formatCSVAmount(line.ProfitMinor), margin, result.Currency,
				result.Completeness, result.Quality,
			}
			if err := writer.Write(record); err != nil {
				return
			}
		}
	}
	writer.Flush()
}

func currentUserID(_ *http.Request) int64 { return 1 }

func formatCSVAmount(minor int64) string {
	return strings.Replace(formatMinorUnits(minor), ".", ",", 1)
}

func formatCSVPercent(basisPoints int64) string {
	return strings.TrimSuffix(strings.Replace(formatMargin(&basisPoints), ".", ",", 1), "%")
}

func safeCSVText(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}
