package main

import (
	"context"
	"strings"
	"testing"
)

func TestCostImportAddsUpdatesAndSkips(t *testing.T) {
	store := testCostStore(t)
	productID := insertCostProduct(t, store, "SKU-1", "5901")

	report, err := store.importCosts(context.Background(), strings.NewReader("sku,unit_purchase_cost,currency\nSKU-1,12.34,PLN\n"))
	if err != nil || report.Added != 1 || len(report.Errors) != 0 {
		t.Fatalf("first report = %+v, err = %v", report, err)
	}
	report, err = store.importCosts(context.Background(), strings.NewReader("ean,unit_purchase_cost,currency\n5901,12.34,PLN\n"))
	if err != nil || report.Skipped != 1 || len(report.Errors) != 0 {
		t.Fatalf("unchanged report = %+v, err = %v", report, err)
	}
	report, err = store.importCosts(context.Background(), strings.NewReader("sku,unit_purchase_cost,currency\nSKU-1,15,PLN\n"))
	if err != nil || report.Updated != 1 || len(report.Errors) != 0 {
		t.Fatalf("update report = %+v, err = %v", report, err)
	}
	var cost int64
	if err := store.db.QueryRow("SELECT unit_cost_minor FROM product_costs WHERE product_id=?", productID).Scan(&cost); err != nil || cost != 1500 {
		t.Fatalf("cost = %d, err = %v", cost, err)
	}
}

func TestCostImportValidationIsAtomicAndReportsRows(t *testing.T) {
	store := testCostStore(t)
	firstID := insertCostProduct(t, store, "SKU-1", "5901")
	insertCostProduct(t, store, "SKU-2", "5902")
	input := "sku,unit_purchase_cost,currency\nSKU-1,10.00,PLN\nUNKNOWN,2.00,PLN\nSKU-1,3.00,PLN\nSKU-2,4.123,PLN\n"

	report, err := store.importCosts(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 3 || report.Errors[0].Row != 3 || report.Errors[1].Row != 4 || report.Errors[2].Row != 5 {
		t.Fatalf("errors = %+v", report.Errors)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM product_costs WHERE product_id=?", firstID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial import count = %d, err = %v", count, err)
	}
}

func TestCostImportRejectsCurrencyDuplicateHeaderAndMalformedCSV(t *testing.T) {
	store := testCostStore(t)
	insertCostProduct(t, store, "SKU-1", "5901")
	for name, input := range map[string]string{
		"currency":         "sku,unit_purchase_cost,currency\nSKU-1,1.00,EUR\n",
		"duplicate header": "sku,sku,unit_purchase_cost,currency\nSKU-1,SKU-1,1.00,PLN\n",
		"malformed":        "sku,unit_purchase_cost,currency\nSKU-1,\"1.00,PLN\n",
	} {
		t.Run(name, func(t *testing.T) {
			report, err := store.importCosts(context.Background(), strings.NewReader(input))
			if err != nil || len(report.Errors) == 0 || report.Errors[0].Row < 1 {
				t.Fatalf("report = %+v, err = %v", report, err)
			}
		})
	}
}

func testCostStore(t *testing.T) *productStore {
	t.Helper()
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return newProductStore(db)
}

func insertCostProduct(t *testing.T, store *productStore, sku, ean string) int64 {
	t.Helper()
	result, err := store.db.Exec("INSERT INTO products(user_id,sku,name,ean) VALUES(1,?,'Product',?)", sku, ean)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
