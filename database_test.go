package main

import (
	"database/sql"
	"testing"
)

func TestMoneyMappingUsesMinorUnits(t *testing.T) {
	tests := map[string]int64{"0": 0, "12.5": 1250, "12.50": 1250, "-0.01": -1, "+7.00": 700}
	for input, want := range tests {
		got, err := parseMinorUnits(input)
		if err != nil || got != want {
			t.Errorf("parseMinorUnits(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"", ".50", "1.001", "1,20", "NaN"} {
		if _, err := parseMinorUnits(input); err == nil {
			t.Errorf("parseMinorUnits(%q) unexpectedly succeeded", input)
		}
	}
	if got := formatMinorUnits(-1); got != "-0.01" {
		t.Fatalf("formatMinorUnits(-1) = %q", got)
	}
}

func TestMigrationsAreIdempotentAndEnforceExternalIDs(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(t.Context(), db); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	mustExec(t, db, `INSERT INTO allegro_integrations(id,user_id,allegro_account_id) VALUES (1,1,'seller')`)
	mustExec(t, db, `INSERT INTO allegro_orders(id,integration_id,allegro_order_id,status,currency,bought_at,source_updated_at) VALUES (1,1,'checkout-1','READY_FOR_PROCESSING','PLN','2026-01-01','2026-01-01')`)
	if _, err := db.Exec(`INSERT INTO allegro_orders(integration_id,allegro_order_id,status,currency,bought_at,source_updated_at) VALUES (1,'checkout-1','READY_FOR_PROCESSING','PLN','2026-01-01','2026-01-01')`); err == nil {
		t.Fatal("duplicate Allegro order was accepted")
	}
	mustExec(t, db, `INSERT INTO allegro_fees(integration_id,order_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at) VALUES (1,1,'fee-1','commission','Commission',-123,'PLN','2026-01-01')`)
	if _, err := db.Exec(`INSERT INTO allegro_fees(integration_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at) VALUES (1,'fee-1','commission','Commission',-123,'PLN','2026-01-01')`); err == nil {
		t.Fatal("duplicate Allegro fee was accepted")
	}
}

func TestDatabaseConstraints(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec(t, db, `INSERT INTO products(id,user_id,sku,name) VALUES (10,1,'SKU-1','Coffee')`)
	if _, err := db.Exec(`INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source) VALUES (10,-1,'PLN','2026-01-01','manual')`); err == nil {
		t.Fatal("negative product cost was accepted")
	}
	if _, err := db.Exec(`INSERT INTO products(user_id,sku,name) VALUES (999,'orphan','Orphan')`); err == nil {
		t.Fatal("orphan product was accepted")
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
