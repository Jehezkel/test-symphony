package main

import (
	"context"
	"testing"
)

func minor(value int64) *int64 { return &value }

func TestCalculateProfitabilityScenarios(t *testing.T) {
	tests := []struct {
		name                              string
		input                             profitabilityInput
		wantRevenue, wantCost, wantProfit int64
		wantCompleteness, wantQuality     string
	}{
		{"regular sale", profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", BuyerDeliveryMinor: 1000, SellerShippingCostMinor: minor(800), BillingValuesMinor: []int64{-1200}, Lines: []profitabilityLineInput{{ID: 1, Quantity: 2, UnitPriceMinor: 5000, UnitPurchaseCostMinor: minor(2500)}}}, 11000, 7000, 4000, "complete", "final"},
		{"seller discount is already in price", profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", SellerShippingCostMinor: minor(0), Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 8000, DiscountMinor: 2000, UnitPurchaseCostMinor: minor(3000)}}}, 8000, 3000, 5000, "complete", "final"},
		{"return is estimated and corrected", profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", Returned: true, SellerShippingCostMinor: minor(500), RevenueCorrectionMinor: -10000, CostCorrectionMinor: 700, BillingValuesMinor: []int64{-1000, 1000}, Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 10000, UnitPurchaseCostMinor: minor(4000)}}}, 0, 5200, -5200, "complete", "estimated_return"},
		{"cancelled is excluded", profitabilityInput{Currency: "PLN", Status: "CANCELLED", BuyerDeliveryMinor: 1000, Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 10000}}}, 0, 0, 0, "complete", "excluded_cancelled"},
		{"missing product and shipping costs", profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 10000}}}, 10000, 0, 10000, "missing_multiple_costs", "final"},
		{"late fee correction", profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", SellerShippingCostMinor: minor(0), BillingValuesMinor: []int64{-1500, 300}, Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 10000, UnitPurchaseCostMinor: minor(4000)}}}, 10000, 5200, 4800, "complete", "final"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculateProfitability(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.RevenueMinor != test.wantRevenue || got.CostMinor != test.wantCost || got.ProfitMinor != test.wantProfit || got.Completeness != test.wantCompleteness || got.Quality != test.wantQuality {
				t.Fatalf("got revenue=%d cost=%d profit=%d completeness=%s quality=%s", got.RevenueMinor, got.CostMinor, got.ProfitMinor, got.Completeness, got.Quality)
			}
			var revenue, cost, profit int64
			for _, line := range got.Lines {
				revenue += line.RevenueMinor
				cost += line.CostMinor
				profit += line.ProfitMinor
			}
			if revenue != got.RevenueMinor || cost != got.CostMinor || profit != got.ProfitMinor {
				t.Fatalf("line sums (%d,%d,%d) differ from aggregate (%d,%d,%d)", revenue, cost, profit, got.RevenueMinor, got.CostMinor, got.ProfitMinor)
			}
		})
	}
}

func TestCalculateProfitabilityAllocationAndIdempotency(t *testing.T) {
	in := profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", BuyerDeliveryMinor: 101, SellerShippingCostMinor: minor(99), BillingValuesMinor: []int64{-101}, Lines: []profitabilityLineInput{{ID: 1, OfferID: 10, Quantity: 1, UnitPriceMinor: 100, UnitPurchaseCostMinor: minor(10)}, {ID: 2, OfferID: 20, Quantity: 1, UnitPriceMinor: 200, UnitPurchaseCostMinor: minor(20)}}}
	first, err := calculateProfitability(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := calculateProfitability(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProfitMinor != second.ProfitMinor || first.Lines[0] != second.Lines[0] || first.Lines[1] != second.Lines[1] {
		t.Fatal("calculation is not deterministic")
	}
	if first.Lines[0].DeliveryRevenueMinor+first.Lines[1].DeliveryRevenueMinor != 101 || first.Lines[0].AllegroCostMinor+first.Lines[1].AllegroCostMinor != 101 {
		t.Fatal("allocation did not preserve totals")
	}

	in.Lines[0].UnitPurchaseCostMinor = minor(40)
	changed, err := calculateProfitability(in)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ProfitMinor != first.ProfitMinor-30 {
		t.Fatalf("profit after cost change = %d, want %d", changed.ProfitMinor, first.ProfitMinor-30)
	}
}

func TestMarginUsesIntegerRounding(t *testing.T) {
	got, err := calculateProfitability(profitabilityInput{Currency: "PLN", Status: "READY_FOR_PROCESSING", SellerShippingCostMinor: minor(0), Lines: []profitabilityLineInput{{ID: 1, Quantity: 1, UnitPriceMinor: 300, UnitPurchaseCostMinor: minor(200)}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.MarginBasisPoints == nil || *got.MarginBasisPoints != 3333 {
		t.Fatalf("margin = %v, want 3333", got.MarginBasisPoints)
	}
}

func TestEngineRecalculatesAfterProductCostAndFeeChange(t *testing.T) {
	db, err := openDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO allegro_integrations(id,user_id,allegro_account_id) VALUES(1,1,'account')`)
	mustExec(`INSERT INTO products(id,user_id,sku,name) VALUES(1,1,'sku','Product')`)
	mustExec(`INSERT INTO allegro_offers(id,integration_id,product_id,allegro_offer_id,name,status) VALUES(1,1,1,'offer','Product','ACTIVE')`)
	mustExec(`INSERT INTO allegro_orders(id,integration_id,allegro_order_id,status,currency,buyer_delivery_minor,seller_shipping_cost_minor,bought_at,source_updated_at) VALUES(1,1,'order','READY_FOR_PROCESSING','PLN',0,0,'2026-01-10T00:00:00Z','2026-01-10T00:00:00Z')`)
	mustExec(`INSERT INTO order_items(id,order_id,offer_id,allegro_line_item_id,allegro_offer_id,name,quantity,unit_price_minor,currency,bought_at) VALUES(1,1,1,'line','offer','Product',1,10000,'PLN','2026-01-10T00:00:00Z')`)
	mustExec(`INSERT INTO product_costs(product_id,unit_cost_minor,currency,valid_from,source) VALUES(1,4000,'PLN','2026-01-01T00:00:00Z','manual')`)
	mustExec(`INSERT INTO allegro_fees(integration_id,order_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at) VALUES(1,1,'fee','commission','Commission',-1000,'PLN','2026-01-10T00:00:00Z')`)
	engine := newProfitabilityEngine(db)
	first, err := engine.CalculateOrder(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProfitMinor != 5000 {
		t.Fatalf("initial profit=%d", first.ProfitMinor)
	}
	mustExec(`UPDATE product_costs SET unit_cost_minor=4500 WHERE product_id=1`)
	mustExec(`UPDATE allegro_fees SET value_minor=-1200 WHERE allegro_fee_id='fee'`)
	second, err := engine.CalculateOrder(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.ProfitMinor != 4300 {
		t.Fatalf("recalculated profit=%d", second.ProfitMinor)
	}
	period, err := engine.CalculatePeriod(context.Background(), 1, "2026-01-01", "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(period) != 1 || period[0].ProfitMinor != second.ProfitMinor {
		t.Fatalf("period results=%v", period)
	}
}
