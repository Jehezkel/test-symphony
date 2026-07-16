package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const allegroPageSize = 100

type syncStatus struct {
	Status, Phase, Trigger, Error string
	Processed                     int
	StartedAt, FinishedAt         string
}

type allegroMoney struct{ Amount, Currency string }
type allegroOffer struct {
	ID, Name, Status string
	External         *struct {
		ID string `json:"id"`
	} `json:"external"`
	Publication struct {
		Status string `json:"status"`
	} `json:"publication"`
	SellingMode struct {
		Price allegroMoney `json:"price"`
	} `json:"sellingMode"`
	UpdatedAt string `json:"updatedAt"`
}
type allegroOrder struct {
	ID, Status, UpdatedAt string
	Fulfillment           struct {
		Status string `json:"status"`
	} `json:"fulfillment"`
	Delivery struct {
		Cost allegroMoney `json:"cost"`
	} `json:"delivery"`
	Payment *struct {
		PaidAmount allegroMoney `json:"paidAmount"`
	} `json:"payment"`
	Summary struct {
		TotalToPay allegroMoney `json:"totalToPay"`
	} `json:"summary"`
	LineItems []struct {
		ID, Name, BoughtAt string
		Offer              struct {
			ID       string `json:"id"`
			External *struct {
				ID string `json:"id"`
			} `json:"external"`
		} `json:"offer"`
		Quantity       int          `json:"quantity"`
		Price          allegroMoney `json:"price"`
		Reconciliation *struct {
			Value allegroMoney `json:"value"`
		} `json:"reconciliation"`
		Discounts json.RawMessage `json:"discounts"`
	} `json:"lineItems"`
}
type allegroFee struct {
	ID         string                    `json:"id"`
	Type       struct{ ID, Name string } `json:"type"`
	Value      allegroMoney              `json:"value"`
	OccurredAt string                    `json:"occurredAt"`
	Order      *struct {
		ID string `json:"id"`
	} `json:"order"`
	Offer *struct {
		ID string `json:"id"`
	} `json:"offer"`
}

func (s *allegroService) apiJSON(ctx context.Context, userID int64, path string, dst any) error {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		resp, err := s.doAPI(ctx, userID, http.MethodGet, path)
		if err != nil {
			last = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			resp.Body.Close()
			if readErr != nil {
				return fmt.Errorf("read Allegro response: %w", readErr)
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if err := json.Unmarshal(body, dst); err != nil {
					return fmt.Errorf("decode Allegro response: %w", err)
				}
				return nil
			}
			last = fmt.Errorf("Allegro API returned status %d", resp.StatusCode)
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return last
			}
		}
		if attempt < 4 {
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return last
}

func (s *allegroService) synchronize(ctx context.Context, userID int64, trigger string) error {
	i, err := s.load(ctx, userID)
	if err != nil {
		return err
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	now := s.now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO allegro_sync_runs(integration_id,trigger,status,started_at) VALUES (?,?, 'running',?)`, i.ID, trigger, now)
	if err != nil {
		return err
	}
	runID, _ := result.LastInsertId()
	processed := 0
	fail := func(e error) error {
		_, _ = s.db.ExecContext(context.Background(), `UPDATE allegro_sync_runs SET status='failed',error_message=?,processed_count=?,finished_at=? WHERE id=?`, e.Error(), processed, s.now().UTC().Format(time.RFC3339Nano), runID)
		return e
	}
	steps := []struct {
		name string
		fn   func(context.Context, int64, int64) (int, error)
	}{{"offers", s.syncOffers}, {"orders", s.syncOrders}, {"fees", s.syncFees}}
	for _, step := range steps {
		if _, err := s.db.ExecContext(ctx, `UPDATE allegro_sync_runs SET phase=?,processed_count=? WHERE id=?`, step.name, processed, runID); err != nil {
			return fail(err)
		}
		n, err := step.fn(ctx, userID, i.ID)
		processed += n
		if err != nil {
			return fail(fmt.Errorf("sync %s: %w", step.name, err))
		}
	}
	finished := s.now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `UPDATE allegro_sync_runs SET status='success',phase='complete',processed_count=?,finished_at=? WHERE id=?`, processed, finished, runID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE allegro_integrations SET last_synced_at=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, finished, i.ID)
	}
	return err
}

func pagePath(path string, offset int) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "limit=" + strconv.Itoa(allegroPageSize) + "&offset=" + strconv.Itoa(offset)
}

func (s *allegroService) syncOffers(ctx context.Context, userID, integrationID int64) (int, error) {
	total := 0
	for offset := 0; ; offset += allegroPageSize {
		var page struct {
			Offers     []allegroOffer `json:"offers"`
			TotalCount int            `json:"totalCount"`
		}
		if err := s.apiJSON(ctx, userID, pagePath("/sale/offers?publication.status=ACTIVE", offset), &page); err != nil {
			return total, err
		}
		if err := s.storeOffers(ctx, integrationID, page.Offers); err != nil {
			return total, err
		}
		total += len(page.Offers)
		if len(page.Offers) < allegroPageSize || offset+len(page.Offers) >= page.TotalCount {
			return total, nil
		}
	}
}
func (s *allegroService) storeOffers(ctx context.Context, integrationID int64, items []allegroOffer) error {
	return s.pageTx(ctx, integrationID, "offers", func(tx *sql.Tx) error {
		for _, o := range items {
			if o.ID == "" || o.Name == "" {
				return errors.New("offer is missing id or name")
			}
			amount, err := parseMinorUnits(o.SellingMode.Price.Amount)
			if err != nil {
				return fmt.Errorf("offer %s price: %w", o.ID, err)
			}
			sku := ""
			if o.External != nil {
				sku = o.External.ID
			}
			productKey := sku
			if productKey == "" {
				productKey = "allegro:" + o.ID
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO products(user_id,sku,name,current_price_minor,currency) SELECT user_id,?,?,?,? FROM allegro_integrations WHERE id=? ON CONFLICT(user_id,sku) DO UPDATE SET name=excluded.name,current_price_minor=excluded.current_price_minor,currency=excluded.currency,updated_at=CURRENT_TIMESTAMP`, productKey, o.Name, amount, o.SellingMode.Price.Currency, integrationID)
			if err != nil {
				return err
			}
			var productID int64
			if err = tx.QueryRowContext(ctx, `SELECT p.id FROM products p JOIN allegro_integrations i ON i.user_id=p.user_id WHERE i.id=? AND p.sku=?`, integrationID, productKey).Scan(&productID); err != nil {
				return err
			}
			status := o.Publication.Status
			if status == "" {
				status = o.Status
			}
			if status == "" {
				return fmt.Errorf("offer %s is missing publication status", o.ID)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO allegro_offers(integration_id,product_id,allegro_offer_id,external_sku,name,status,current_price_minor,currency,source_updated_at,synced_at) VALUES(?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(integration_id,allegro_offer_id) DO UPDATE SET product_id=excluded.product_id,external_sku=excluded.external_sku,name=excluded.name,status=excluded.status,current_price_minor=excluded.current_price_minor,currency=excluded.currency,source_updated_at=excluded.source_updated_at,synced_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP`, integrationID, productID, o.ID, nullString(sku), o.Name, status, amount, o.SellingMode.Price.Currency, nullString(o.UpdatedAt))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *allegroService) syncOrders(ctx context.Context, userID, integrationID int64) (int, error) {
	total := 0
	for offset := 0; ; offset += allegroPageSize {
		var page struct {
			CheckoutForms []allegroOrder `json:"checkoutForms"`
			TotalCount    int            `json:"totalCount"`
		}
		if err := s.apiJSON(ctx, userID, pagePath("/order/checkout-forms", offset), &page); err != nil {
			return total, err
		}
		if err := s.storeOrders(ctx, integrationID, page.CheckoutForms); err != nil {
			return total, err
		}
		total += len(page.CheckoutForms)
		if len(page.CheckoutForms) < allegroPageSize || offset+len(page.CheckoutForms) >= page.TotalCount {
			return total, nil
		}
	}
}
func (s *allegroService) storeOrders(ctx context.Context, integrationID int64, items []allegroOrder) error {
	return s.pageTx(ctx, integrationID, "orders", func(tx *sql.Tx) error {
		for _, o := range items {
			if o.ID == "" || len(o.LineItems) == 0 {
				return errors.New("order is missing id or line items")
			}
			currency := o.LineItems[0].Price.Currency
			delivery, err := parseMinorUnits(o.Delivery.Cost.Amount)
			if err != nil {
				return err
			}
			paid := sql.NullInt64{}
			if o.Payment != nil {
				v, e := parseMinorUnits(o.Payment.PaidAmount.Amount)
				if e != nil {
					return e
				}
				paid = sql.NullInt64{Int64: v, Valid: true}
			}
			totalPay, e := parseMinorUnits(o.Summary.TotalToPay.Amount)
			if e != nil {
				return e
			}
			bought := o.LineItems[0].BoughtAt
			var orderID int64
			e = tx.QueryRowContext(ctx, `INSERT INTO allegro_orders(integration_id,allegro_order_id,status,fulfillment_status,currency,buyer_delivery_minor,paid_amount_minor,total_to_pay_minor,bought_at,source_updated_at,synced_at) VALUES(?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(integration_id,allegro_order_id) DO UPDATE SET status=excluded.status,fulfillment_status=excluded.fulfillment_status,currency=excluded.currency,buyer_delivery_minor=excluded.buyer_delivery_minor,paid_amount_minor=excluded.paid_amount_minor,total_to_pay_minor=excluded.total_to_pay_minor,bought_at=excluded.bought_at,source_updated_at=excluded.source_updated_at,synced_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP RETURNING id`, integrationID, o.ID, o.Status, nullString(o.Fulfillment.Status), currency, delivery, paid, totalPay, bought, o.UpdatedAt).Scan(&orderID)
			if e != nil {
				return e
			}
			for _, li := range o.LineItems {
				price, e := parseMinorUnits(li.Price.Amount)
				if e != nil {
					return e
				}
				recon := int64(0)
				if li.Reconciliation != nil {
					recon, e = parseMinorUnits(li.Reconciliation.Value.Amount)
					if e != nil {
						return e
					}
				}
				discounts := string(li.Discounts)
				if discounts == "" {
					discounts = "[]"
				}
				sku := ""
				if li.Offer.External != nil {
					sku = li.Offer.External.ID
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO order_items(order_id,offer_id,allegro_line_item_id,allegro_offer_id,external_sku,name,quantity,unit_price_minor,currency,reconciliation_minor,bought_at,discounts_json) VALUES(?,(SELECT id FROM allegro_offers WHERE integration_id=? AND allegro_offer_id=?),?,?,?,?,?,?,?,?,?,?) ON CONFLICT(order_id,allegro_line_item_id) DO UPDATE SET offer_id=excluded.offer_id,external_sku=excluded.external_sku,name=excluded.name,quantity=excluded.quantity,unit_price_minor=excluded.unit_price_minor,currency=excluded.currency,reconciliation_minor=excluded.reconciliation_minor,bought_at=excluded.bought_at,discounts_json=excluded.discounts_json,updated_at=CURRENT_TIMESTAMP`, orderID, integrationID, li.Offer.ID, li.ID, li.Offer.ID, nullString(sku), li.Name, li.Quantity, price, li.Price.Currency, recon, li.BoughtAt, discounts)
				if e != nil {
					return e
				}
			}
		}
		return nil
	})
}

func (s *allegroService) syncFees(ctx context.Context, userID, integrationID int64) (int, error) {
	total := 0
	for offset := 0; ; offset += allegroPageSize {
		var page struct {
			BillingEntries []allegroFee `json:"billingEntries"`
			TotalCount     int          `json:"totalCount"`
		}
		if err := s.apiJSON(ctx, userID, pagePath("/billing/billing-entries", offset), &page); err != nil {
			return total, err
		}
		if err := s.storeFees(ctx, integrationID, page.BillingEntries); err != nil {
			return total, err
		}
		total += len(page.BillingEntries)
		if len(page.BillingEntries) < allegroPageSize || offset+len(page.BillingEntries) >= page.TotalCount {
			return total, nil
		}
	}
}
func (s *allegroService) storeFees(ctx context.Context, integrationID int64, items []allegroFee) error {
	return s.pageTx(ctx, integrationID, "fees", func(tx *sql.Tx) error {
		for _, f := range items {
			if f.ID == "" || f.Type.ID == "" {
				return errors.New("fee is missing id or type")
			}
			value, err := parseMinorUnits(f.Value.Amount)
			if err != nil {
				return err
			}
			order, offer := "", ""
			if f.Order != nil {
				order = f.Order.ID
			}
			if f.Offer != nil {
				offer = f.Offer.ID
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO allegro_fees(integration_id,order_id,offer_id,allegro_fee_id,type_id,type_name,value_minor,currency,occurred_at,synced_at) VALUES(?,(SELECT id FROM allegro_orders WHERE integration_id=? AND allegro_order_id=?),(SELECT id FROM allegro_offers WHERE integration_id=? AND allegro_offer_id=?),?,?,?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(integration_id,allegro_fee_id) DO UPDATE SET order_id=excluded.order_id,offer_id=excluded.offer_id,type_id=excluded.type_id,type_name=excluded.type_name,value_minor=excluded.value_minor,currency=excluded.currency,occurred_at=excluded.occurred_at,synced_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP`, integrationID, integrationID, order, integrationID, offer, f.ID, f.Type.ID, f.Type.Name, value, f.Value.Currency, f.OccurredAt)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *allegroService) pageTx(ctx context.Context, integrationID int64, resource string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO allegro_sync_checkpoints(integration_id,resource,last_success_at) VALUES(?,?,?) ON CONFLICT(integration_id,resource) DO UPDATE SET last_success_at=excluded.last_success_at,updated_at=CURRENT_TIMESTAMP`, integrationID, resource, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *allegroService) latestSync(ctx context.Context, userID int64) syncStatus {
	var st syncStatus
	var finished, errText sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT r.status,r.phase,r.trigger,r.processed_count,r.started_at,r.finished_at,r.error_message FROM allegro_sync_runs r JOIN allegro_integrations i ON i.id=r.integration_id WHERE i.user_id=? ORDER BY r.id DESC LIMIT 1`, userID).Scan(&st.Status, &st.Phase, &st.Trigger, &st.Processed, &st.StartedAt, &finished, &errText)
	if err == nil {
		st.FinishedAt = finished.String
		st.Error = errText.String
	}
	return st
}
func (s *allegroService) startScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				userIDs, err := s.scheduledUserIDs(ctx)
				if err != nil {
					continue
				}
				for _, userID := range userIDs {
					runCtx, cancel := context.WithTimeout(ctx, interval)
					_ = s.synchronize(runCtx, userID, "scheduled")
					cancel()
				}
			}
		}
	}()
}

func (s *allegroService) scheduledUserIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM allegro_integrations WHERE access_token_ciphertext IS NOT NULL ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, rows.Err()
}
