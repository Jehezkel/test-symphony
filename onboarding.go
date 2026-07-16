package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
)

type onboardingProgress struct {
	Connected, Synced, CostsComplete, ReportOpened bool
	SyncRunning, SyncFailed                        bool
	SyncError                                      string
	OfferCount, MissingCostCount                   int
}

func (p onboardingProgress) readyForReport() bool {
	return p.Connected && p.Synced && p.CostsComplete
}

func (p onboardingProgress) complete() bool {
	return p.readyForReport() && p.ReportOpened
}

func (a *app) loadOnboardingProgress(ctx context.Context, userID int64) (onboardingProgress, error) {
	var p onboardingProgress
	var integrationID int64
	err := a.products.db.QueryRowContext(ctx, `SELECT id FROM allegro_integrations WHERE user_id=? AND access_token_ciphertext IS NOT NULL ORDER BY updated_at DESC LIMIT 1`, userID).Scan(&integrationID)
	if err == sql.ErrNoRows {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("load onboarding integration: %w", err)
	}
	p.Connected = true

	var syncState string
	var syncError sql.NullString
	err = a.products.db.QueryRowContext(ctx, `SELECT status,error_message FROM allegro_sync_runs WHERE integration_id=? ORDER BY id DESC LIMIT 1`, integrationID).Scan(&syncState, &syncError)
	if err != nil && err != sql.ErrNoRows {
		return p, fmt.Errorf("load onboarding sync: %w", err)
	}
	p.SyncRunning = syncState == "running"
	p.SyncFailed = syncState == "failed"
	p.SyncError = syncError.String
	if err := a.products.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM allegro_sync_runs WHERE integration_id=? AND status='success')`, integrationID).Scan(&p.Synced); err != nil {
		return p, fmt.Errorf("load successful onboarding sync: %w", err)
	}

	if err := a.products.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE o.product_id IS NULL OR NOT EXISTS (
		SELECT 1 FROM product_costs pc WHERE pc.product_id=o.product_id AND pc.valid_from<=CURRENT_TIMESTAMP
	)) FROM allegro_offers o WHERE o.integration_id=?`, integrationID).Scan(&p.OfferCount, &p.MissingCostCount); err != nil {
		return p, fmt.Errorf("load onboarding costs: %w", err)
	}
	p.CostsComplete = p.Synced && p.OfferCount > 0 && p.MissingCostCount == 0
	if err := a.products.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_report_views WHERE user_id=?)`, userID).Scan(&p.ReportOpened); err != nil {
		return p, fmt.Errorf("load onboarding report view: %w", err)
	}
	return p, nil
}

func (a *app) onboarding(w http.ResponseWriter, r *http.Request) {
	progress, err := a.loadOnboardingProgress(r.Context(), requestUserID(r))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = onboardingPage(onboardingProgress{}, "Nie udało się wczytać postępu. Odśwież stronę i spróbuj ponownie.").Render(r.Context(), w)
		return
	}
	if progress.complete() {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if err := onboardingPage(progress, "").Render(r.Context(), w); err != nil {
		http.Error(w, "render onboarding", http.StatusInternalServerError)
	}
}
