package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

const baselineCostDate = "1970-01-01T00:00:00Z"

type productCostItem struct {
	ID       int64
	SKU      string
	EAN      string
	Name     string
	Cost     string
	Currency string
	Offers   string
	Missing  bool
}

type costImportError struct {
	Row     int
	Message string
}

type costImportReport struct {
	Added   int
	Updated int
	Skipped int
	Errors  []costImportError
}

type parsedCostRow struct {
	row       int
	productID int64
	cost      int64
	currency  string
}

func (s *productStore) listCosts(ctx context.Context, userID int64) ([]productCostItem, error) {
	// Older synchronized offers may predate automatic product association.
	if _, err := s.db.ExecContext(ctx, `UPDATE allegro_offers
		SET product_id = (SELECT p.id FROM products p JOIN allegro_integrations i ON i.user_id=p.user_id WHERE i.id=allegro_offers.integration_id AND (p.sku=allegro_offers.external_sku OR p.ean=allegro_offers.external_sku) ORDER BY p.id LIMIT 1)
		WHERE product_id IS NULL AND external_sku IS NOT NULL
		AND integration_id IN (SELECT id FROM allegro_integrations WHERE user_id=?)`, userID); err != nil {
		return nil, fmt.Errorf("associate offers: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.sku,COALESCE(p.ean,''),p.name,
		COALESCE((SELECT pc.unit_cost_minor FROM product_costs pc WHERE pc.product_id=p.id AND pc.valid_from<=CURRENT_TIMESTAMP ORDER BY pc.valid_from DESC,pc.id DESC LIMIT 1),-1),
		COALESCE((SELECT pc.currency FROM product_costs pc WHERE pc.product_id=p.id AND pc.valid_from<=CURRENT_TIMESTAMP ORDER BY pc.valid_from DESC,pc.id DESC LIMIT 1),''),
		COALESCE(GROUP_CONCAT(o.allegro_offer_id || ' — ' || o.name, ' | '),'')
		FROM products p LEFT JOIN allegro_offers o ON o.product_id=p.id
		WHERE p.user_id=? GROUP BY p.id ORDER BY p.sku`, userID)
	if err != nil {
		return nil, fmt.Errorf("list product costs: %w", err)
	}
	defer rows.Close()
	var items []productCostItem
	for rows.Next() {
		var item productCostItem
		var cost int64
		if err := rows.Scan(&item.ID, &item.SKU, &item.EAN, &item.Name, &cost, &item.Currency, &item.Offers); err != nil {
			return nil, fmt.Errorf("scan product cost: %w", err)
		}
		item.Missing = cost < 0
		if !item.Missing {
			item.Cost = formatMinorUnits(cost)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *productStore) setCost(ctx context.Context, userID, productID, cost int64, currency, source string) error {
	if cost < 0 || currency != "PLN" || (source != "manual" && source != "import") {
		return errors.New("invalid product cost")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source,source_key)
		SELECT id,?,?,?,?,? FROM products WHERE id=? AND user_id=?
		ON CONFLICT(product_id,valid_from,currency) DO UPDATE SET unit_cost_minor=excluded.unit_cost_minor,source=excluded.source,source_key=excluded.source_key,updated_at=CURRENT_TIMESTAMP`, cost, currency, baselineCostDate, source, source, productID, userID)
	if err != nil {
		return fmt.Errorf("set product cost: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("product cost rows: %w", err)
	} else if n == 0 {
		return errProductNotFound
	}
	return nil
}

func (s *productStore) importCosts(ctx context.Context, userID int64, input io.Reader) (costImportReport, error) {
	report := costImportReport{}
	reader := csv.NewReader(input)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		row := 1
		var parseErr *csv.ParseError
		if errors.As(err, &parseErr) && parseErr.Line > 0 {
			row = parseErr.Line
		}
		report.Errors = append(report.Errors, costImportError{Row: row, Message: "Nieprawidłowy format CSV: " + err.Error()})
		return report, nil
	}
	if len(records) == 0 {
		report.Errors = append(report.Errors, costImportError{Row: 1, Message: "Plik jest pusty."})
		return report, nil
	}
	headers := map[string]int{}
	for i, value := range records[0] {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
		if _, exists := headers[name]; name != "" && exists {
			report.Errors = append(report.Errors, costImportError{Row: 1, Message: "Zduplikowana kolumna " + name + "."})
			continue
		}
		headers[name] = i
	}
	if _, ok := headers["unit_purchase_cost"]; !ok {
		report.Errors = append(report.Errors, costImportError{Row: 1, Message: "Brak kolumny unit_purchase_cost."})
	}
	if _, ok := headers["currency"]; !ok {
		report.Errors = append(report.Errors, costImportError{Row: 1, Message: "Brak kolumny currency."})
	}
	if !hasAnyHeader(headers, "sku", "external_id", "ean", "offer_id") {
		report.Errors = append(report.Errors, costImportError{Row: 1, Message: "Wymagana jest kolumna sku, external_id, ean lub offer_id."})
	}
	if len(report.Errors) > 0 {
		return report, nil
	}

	seen := map[int64]int{}
	rows := make([]parsedCostRow, 0, len(records)-1)
	for index, record := range records[1:] {
		rowNumber := index + 2
		if blankRecord(record) {
			report.Skipped++
			continue
		}
		productID, resolveErr := s.resolveCostProduct(ctx, userID, headers, record)
		if resolveErr != nil {
			report.Errors = append(report.Errors, costImportError{Row: rowNumber, Message: resolveErr.Error()})
			continue
		}
		if previous, duplicate := seen[productID]; duplicate {
			report.Errors = append(report.Errors, costImportError{Row: rowNumber, Message: fmt.Sprintf("Duplikat produktu (pierwszy wiersz: %d).", previous)})
			continue
		}
		seen[productID] = rowNumber
		currency := strings.ToUpper(field(record, headers["currency"]))
		if currency != "PLN" {
			report.Errors = append(report.Errors, costImportError{Row: rowNumber, Message: "Waluta musi być równa PLN."})
			continue
		}
		cost, parseErr := parseMinorUnits(field(record, headers["unit_purchase_cost"]))
		if parseErr != nil || cost < 0 {
			report.Errors = append(report.Errors, costImportError{Row: rowNumber, Message: "Koszt musi być nieujemną liczbą z kropką i maksymalnie 2 miejscami po przecinku."})
			continue
		}
		rows = append(rows, parsedCostRow{row: rowNumber, productID: productID, cost: cost, currency: currency})
	}
	if len(report.Errors) > 0 {
		report.Skipped += len(rows)
		return report, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("begin cost import: %w", err)
	}
	defer tx.Rollback()
	for _, row := range rows {
		var current int64
		err := tx.QueryRowContext(ctx, `SELECT unit_cost_minor FROM product_costs WHERE product_id=? AND valid_from=? AND currency=?`, row.productID, baselineCostDate, row.currency).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err = tx.ExecContext(ctx, `INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source,source_key) VALUES(?,?,?,?, 'import',?)`, row.productID, row.cost, row.currency, baselineCostDate, fmt.Sprintf("csv:%d", row.row)); err != nil {
				return report, fmt.Errorf("insert row %d: %w", row.row, err)
			}
			report.Added++
		case err != nil:
			return report, fmt.Errorf("read row %d: %w", row.row, err)
		case current == row.cost:
			report.Skipped++
		default:
			if _, err = tx.ExecContext(ctx, `UPDATE product_costs SET unit_cost_minor=?,source='import',source_key=?,updated_at=CURRENT_TIMESTAMP WHERE product_id=? AND valid_from=? AND currency=?`, row.cost, fmt.Sprintf("csv:%d", row.row), row.productID, baselineCostDate, row.currency); err != nil {
				return report, fmt.Errorf("update row %d: %w", row.row, err)
			}
			report.Updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit cost import: %w", err)
	}
	return report, nil
}

func (s *productStore) resolveCostProduct(ctx context.Context, userID int64, headers map[string]int, record []string) (int64, error) {
	type lookup struct{ column, query string }
	lookups := []lookup{
		{"sku", `SELECT id FROM products WHERE user_id=? AND sku=?`},
		{"external_id", `SELECT id FROM products WHERE user_id=? AND sku=?`},
		{"ean", `SELECT id FROM products WHERE user_id=? AND ean=?`},
		{"offer_id", `SELECT o.product_id FROM allegro_offers o JOIN allegro_integrations i ON i.id=o.integration_id WHERE i.user_id=? AND o.allegro_offer_id=? AND o.product_id IS NOT NULL`},
	}
	provided := false
	for _, item := range lookups {
		index, ok := headers[item.column]
		if !ok || field(record, index) == "" {
			continue
		}
		provided = true
		var id int64
		err := s.db.QueryRowContext(ctx, item.query, userID, field(record, index)).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("Nie można sprawdzić produktu: %v", err)
		}
	}
	if !provided {
		return 0, errors.New("Brak identyfikatora SKU/EAN/oferty.")
	}
	return 0, errors.New("Nieznane SKU, EAN lub ID oferty.")
}

func hasAnyHeader(headers map[string]int, names ...string) bool {
	for _, name := range names {
		if _, ok := headers[name]; ok {
			return true
		}
	}
	return false
}

func field(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func blankRecord(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
