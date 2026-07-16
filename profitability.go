package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const profitabilityCalculationVersion = 1

var errProfitabilityCurrencyMismatch = errors.New("profitability inputs use different currencies")

type profitabilityLineInput struct {
	ID                    int64
	OfferID               int64
	Quantity              int64
	UnitPriceMinor        int64
	DiscountMinor         int64
	ReconciliationMinor   int64
	UnitPurchaseCostMinor *int64
}

type profitabilityInput struct {
	Currency                string
	Status                  string
	Returned                bool
	BuyerDeliveryMinor      int64
	SellerShippingCostMinor *int64
	SurchargeRevenueMinor   int64
	RevenueCorrectionMinor  int64
	CostCorrectionMinor     int64
	BillingValuesMinor      []int64
	Lines                   []profitabilityLineInput
}

type profitabilityLineResult struct {
	ID, OfferID                                               int64
	ProductRevenueMinor, DiscountMinor, ReconciliationMinor   int64
	DeliveryRevenueMinor, SurchargeRevenueMinor               int64
	RevenueCorrectionMinor, AllegroCostMinor                  int64
	PurchaseCostMinor, ShippingCostMinor, CostCorrectionMinor int64
	RevenueMinor, CostMinor, ProfitMinor                      int64
}

type profitabilityResult struct {
	CalculationVersion                                        int
	Currency, Completeness, Quality                           string
	ProductRevenueMinor, DiscountMinor, DeliveryRevenueMinor  int64
	SurchargeRevenueMinor, ReconciliationRevenueMinor         int64
	AdjustmentRevenueMinor, AllegroCostMinor                  int64
	PurchaseCostMinor, ShippingCostMinor, AdjustmentCostMinor int64
	RevenueMinor, CostMinor, ProfitMinor                      int64
	MarginBasisPoints                                         *int64
	Lines                                                     []profitabilityLineResult
}

func calculateProfitability(in profitabilityInput) (profitabilityResult, error) {
	result := profitabilityResult{CalculationVersion: profitabilityCalculationVersion, Currency: in.Currency, Quality: "final"}
	if len(in.Currency) != 3 {
		return result, errProfitabilityCurrencyMismatch
	}
	if in.Status == "CANCELLED" || in.Status == "CANCELED" {
		result.Completeness, result.Quality = "complete", "excluded_cancelled"
		return result, nil
	}
	if in.Returned {
		result.Quality = "estimated_return"
	}

	missingProduct := false
	weights := make([]int64, len(in.Lines))
	quantityWeights := make([]int64, len(in.Lines))
	result.Lines = make([]profitabilityLineResult, len(in.Lines))
	for index, line := range in.Lines {
		if line.Quantity <= 0 {
			return result, fmt.Errorf("line %d has invalid quantity", line.ID)
		}
		productRevenue := line.UnitPriceMinor * line.Quantity
		purchaseCost := int64(0)
		if line.UnitPurchaseCostMinor == nil {
			missingProduct = true
		} else {
			purchaseCost = *line.UnitPurchaseCostMinor * line.Quantity
		}
		result.ProductRevenueMinor += productRevenue
		result.DiscountMinor += line.DiscountMinor
		result.ReconciliationRevenueMinor += line.ReconciliationMinor
		result.PurchaseCostMinor += purchaseCost
		weights[index], quantityWeights[index] = productRevenue, line.Quantity
		result.Lines[index] = profitabilityLineResult{ID: line.ID, OfferID: line.OfferID, ProductRevenueMinor: productRevenue, DiscountMinor: line.DiscountMinor, ReconciliationMinor: line.ReconciliationMinor, PurchaseCostMinor: purchaseCost}
	}

	result.DeliveryRevenueMinor = in.BuyerDeliveryMinor
	result.SurchargeRevenueMinor = in.SurchargeRevenueMinor
	result.AdjustmentRevenueMinor = in.RevenueCorrectionMinor
	result.AdjustmentCostMinor = in.CostCorrectionMinor
	for _, signedValue := range in.BillingValuesMinor {
		result.AllegroCostMinor -= signedValue
	}
	missingShipping := in.SellerShippingCostMinor == nil
	if !missingShipping {
		result.ShippingCostMinor = *in.SellerShippingCostMinor
	}

	sharedRevenue := result.DeliveryRevenueMinor + result.SurchargeRevenueMinor + result.AdjustmentRevenueMinor
	sharedCost := result.AllegroCostMinor + result.ShippingCostMinor + result.AdjustmentCostMinor
	allocationWeights := weights
	if sumInt64(weights) == 0 {
		allocationWeights = quantityWeights
	}
	delivery := allocateMinor(result.DeliveryRevenueMinor, allocationWeights)
	surcharge := allocateMinor(result.SurchargeRevenueMinor, allocationWeights)
	revenueCorrection := allocateMinor(result.AdjustmentRevenueMinor, allocationWeights)
	allegroCost := allocateMinor(result.AllegroCostMinor, allocationWeights)
	shipping := allocateMinor(result.ShippingCostMinor, allocationWeights)
	costCorrection := allocateMinor(result.AdjustmentCostMinor, allocationWeights)
	for index := range result.Lines {
		line := &result.Lines[index]
		line.DeliveryRevenueMinor, line.SurchargeRevenueMinor = delivery[index], surcharge[index]
		line.RevenueCorrectionMinor, line.AllegroCostMinor = revenueCorrection[index], allegroCost[index]
		line.ShippingCostMinor, line.CostCorrectionMinor = shipping[index], costCorrection[index]
		line.RevenueMinor = line.ProductRevenueMinor + line.ReconciliationMinor + delivery[index] + surcharge[index] + revenueCorrection[index]
		line.CostMinor = line.PurchaseCostMinor + allegroCost[index] + shipping[index] + costCorrection[index]
		line.ProfitMinor = line.RevenueMinor - line.CostMinor
	}
	result.RevenueMinor = result.ProductRevenueMinor + result.ReconciliationRevenueMinor + sharedRevenue
	result.CostMinor = result.PurchaseCostMinor + sharedCost
	result.ProfitMinor = result.RevenueMinor - result.CostMinor
	if result.RevenueMinor != 0 && !missingProduct && !missingShipping {
		margin := roundedRatio(result.ProfitMinor*10000, result.RevenueMinor)
		result.MarginBasisPoints = &margin
	}
	switch {
	case missingProduct && missingShipping:
		result.Completeness = "missing_multiple_costs"
	case missingProduct:
		result.Completeness = "missing_product_cost"
	case missingShipping:
		result.Completeness = "missing_shipping_cost"
	default:
		result.Completeness = "complete"
	}
	return result, nil
}

func sumInt64(values []int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}

func allocateMinor(total int64, weights []int64) []int64 {
	allocated := make([]int64, len(weights))
	if len(weights) == 0 {
		return allocated
	}
	weightTotal := sumInt64(weights)
	if weightTotal == 0 {
		allocated[len(allocated)-1] = total
		return allocated
	}
	remaining := total
	for index := 0; index < len(weights)-1; index++ {
		allocated[index] = total * weights[index] / weightTotal
		remaining -= allocated[index]
	}
	allocated[len(allocated)-1] = remaining
	return allocated
}

func roundedRatio(numerator, denominator int64) int64 {
	quotient, remainder := numerator/denominator, numerator%denominator
	if remainder < 0 {
		remainder = -remainder
	}
	absDenominator := denominator
	if absDenominator < 0 {
		absDenominator = -absDenominator
	}
	if remainder*2 >= absDenominator {
		if (numerator < 0) != (denominator < 0) {
			quotient--
		} else {
			quotient++
		}
	}
	return quotient
}

type profitabilityEngine struct{ db *sql.DB }

func newProfitabilityEngine(db *sql.DB) *profitabilityEngine { return &profitabilityEngine{db: db} }

// CalculateOrder always reads the latest effective costs, fees and adjustments.
// It is intentionally side-effect free, so repeated and concurrent reads cannot
// produce stale results after a cost or a late Allegro fee is updated.
func (e *profitabilityEngine) CalculateOrder(ctx context.Context, orderID int64) (profitabilityResult, error) {
	var in profitabilityInput
	var shipping sql.NullInt64
	var fulfillment sql.NullString
	if err := e.db.QueryRowContext(ctx, `SELECT currency,status,fulfillment_status,buyer_delivery_minor,seller_shipping_cost_minor FROM allegro_orders WHERE id=?`, orderID).Scan(&in.Currency, &in.Status, &fulfillment, &in.BuyerDeliveryMinor, &shipping); err != nil {
		return profitabilityResult{}, fmt.Errorf("load profitability order: %w", err)
	}
	if shipping.Valid {
		in.SellerShippingCostMinor = &shipping.Int64
	}
	upperState := strings.ToUpper(in.Status + " " + fulfillment.String)
	in.Returned = strings.Contains(upperState, "RETURN") || strings.Contains(upperState, "REFUND")

	rows, err := e.db.QueryContext(ctx, `SELECT oi.id,COALESCE(oi.offer_id,0),oi.quantity,oi.unit_price_minor,oi.reconciliation_minor,
		(SELECT pc.unit_cost_minor FROM allegro_offers ao JOIN product_costs pc ON pc.product_id=ao.product_id WHERE ao.id=oi.offer_id AND pc.currency=oi.currency AND pc.valid_from<=oi.bought_at ORDER BY pc.valid_from DESC,pc.id DESC LIMIT 1)
		FROM order_items oi WHERE oi.order_id=? ORDER BY oi.id`, orderID)
	if err != nil {
		return profitabilityResult{}, fmt.Errorf("load profitability lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line profitabilityLineInput
		var cost sql.NullInt64
		if err := rows.Scan(&line.ID, &line.OfferID, &line.Quantity, &line.UnitPriceMinor, &line.ReconciliationMinor, &cost); err != nil {
			return profitabilityResult{}, err
		}
		if cost.Valid {
			line.UnitPurchaseCostMinor = &cost.Int64
		}
		in.Lines = append(in.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return profitabilityResult{}, err
	}

	feeRows, err := e.db.QueryContext(ctx, `SELECT value_minor FROM allegro_fees WHERE order_id=? AND currency=? ORDER BY id`, orderID, in.Currency)
	if err != nil {
		return profitabilityResult{}, fmt.Errorf("load profitability fees: %w", err)
	}
	defer feeRows.Close()
	for feeRows.Next() {
		var value int64
		if err := feeRows.Scan(&value); err != nil {
			return profitabilityResult{}, err
		}
		in.BillingValuesMinor = append(in.BillingValuesMinor, value)
	}
	if err := feeRows.Err(); err != nil {
		return profitabilityResult{}, err
	}

	adjustmentRows, err := e.db.QueryContext(ctx, `SELECT kind,value_minor FROM order_adjustments WHERE order_id=? AND currency=? AND status='finalized' ORDER BY id`, orderID, in.Currency)
	if err != nil {
		return profitabilityResult{}, fmt.Errorf("load profitability adjustments: %w", err)
	}
	defer adjustmentRows.Close()
	for adjustmentRows.Next() {
		var kind string
		var value int64
		if err := adjustmentRows.Scan(&kind, &value); err != nil {
			return profitabilityResult{}, err
		}
		switch kind {
		case "surcharge":
			in.SurchargeRevenueMinor += value
		case "reconciliation":
			in.RevenueCorrectionMinor += value
		case "revenue_correction":
			in.RevenueCorrectionMinor += value
		case "cost_correction":
			in.CostCorrectionMinor += value
		}
	}
	if err := adjustmentRows.Err(); err != nil {
		return profitabilityResult{}, err
	}
	return calculateProfitability(in)
}

func (e *profitabilityEngine) CalculatePeriod(ctx context.Context, integrationID int64, from, to string) ([]profitabilityResult, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT id FROM allegro_orders WHERE integration_id=? AND bought_at>=? AND bought_at<? ORDER BY bought_at,id`, integrationID, from, to)
	if err != nil {
		return nil, fmt.Errorf("load profitability period: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results := make([]profitabilityResult, 0, len(ids))
	for _, id := range ids {
		result, err := e.CalculateOrder(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
