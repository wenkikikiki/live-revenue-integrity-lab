package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
)

const sessionClosedTopic = "live.session.closed.v1"

// ContributorSummary reports top contributor totals for a live session.
type ContributorSummary struct {
	ViewerID uint64 `json:"viewer_id"`
	Coins    int64  `json:"coins"`
}

// SettlementResponse is returned by settlement read APIs.
type SettlementResponse struct {
	LiveSessionID         uint64               `json:"live_session_id"`
	GrossCoinSpend        int64                `json:"gross_coin_spend"`
	AcceptedGiftCount     int64                `json:"accepted_gift_count"`
	DiamondRewardTotal    int64                `json:"diamond_reward_total"`
	TopContributors       []ContributorSummary `json:"top_contributors"`
	ReconciliationStatus  string               `json:"reconciliation_status"`
	WalletGiftDebitTotal  int64                `json:"wallet_gift_debit_total"`
	GiftOrderCoinTotal    int64                `json:"gift_order_coin_total"`
	MismatchCount         int                  `json:"mismatch_count"`
	ReconciliationDetails string               `json:"reconciliation_details_json"`
}

// CloseLiveSession closes an OPEN live session and emits a close event.
func (s *Service) CloseLiveSession(ctx context.Context, liveSessionID uint64) *AppError {
	if liveSessionID == 0 {
		return errValidation
	}
	payload, err := events.MarshalJSONString(events.LiveSessionClosedV1{
		LiveSessionID: liveSessionID,
		ClosedAtMs:    time.Now().UTC().UnixMilli(),
	})
	if err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	defer func() {
		_ = tx.Rollback()
	}()

	res, err := tx.ExecContext(ctx, `
		UPDATE live_sessions
		SET status = 'CLOSED', closed_at = ?
		WHERE live_session_id = ? AND status = 'OPEN'
	`, time.Now().UTC(), liveSessionID)
	if err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	if affected == 0 {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM live_sessions WHERE live_session_id = ?`, liveSessionID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return &AppError{HTTPStatus: 404, Code: "LIVE_SESSION_NOT_FOUND", Message: "live session not found"}
		}
		if err != nil {
			return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
		}
		if status == "CLOSED" {
			return &AppError{HTTPStatus: 409, Code: "LIVE_SESSION_CLOSED", Message: "live session already closed"}
		}
		return &AppError{HTTPStatus: 409, Code: "LIVE_SESSION_STATE_INVALID", Message: "session state invalid"}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, sessionClosedTopic, strconv.FormatUint(liveSessionID, 10), payload, time.Now().UTC()); err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	if err := tx.Commit(); err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	return nil
}

// GenerateSettlement computes deterministic settlement and reconciliation rows.
func (s *Service) GenerateSettlement(ctx context.Context, liveSessionID uint64) (SettlementResponse, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SettlementResponse{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM live_sessions WHERE live_session_id = ?`, liveSessionID).Scan(&exists); err != nil {
		return SettlementResponse{}, err
	}

	var grossSpend, giftCount, diamondTotal int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(charged_coins), 0), COUNT(*), COALESCE(SUM(diamond_reward), 0)
		FROM gift_orders
		WHERE live_session_id = ? AND status = 'ACCEPTED'
	`, liveSessionID).Scan(&grossSpend, &giftCount, &diamondTotal); err != nil {
		return SettlementResponse{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO stream_settlements (live_session_id, gross_coin_spend, accepted_gift_count, diamond_reward_total, generated_at)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			gross_coin_spend = VALUES(gross_coin_spend),
			accepted_gift_count = VALUES(accepted_gift_count),
			diamond_reward_total = VALUES(diamond_reward_total),
			generated_at = VALUES(generated_at)
	`, liveSessionID, grossSpend, giftCount, diamondTotal, time.Now().UTC()); err != nil {
		return SettlementResponse{}, err
	}

	var walletDebitTotal int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(-we.amount), 0)
		FROM gift_orders go
		JOIN wallet_transactions wt
		  ON wt.actor_user_id = go.viewer_id
		 AND wt.request_id = go.request_id
		 AND wt.tx_type = 'GIFT_DEBIT'
		JOIN wallet_entries we
		  ON we.tx_id = wt.tx_id
		WHERE go.live_session_id = ?
		  AND go.status = 'ACCEPTED'
		  AND we.account_code LIKE 'USER_COINS:%'
	`, liveSessionID).Scan(&walletDebitTotal); err != nil {
		return SettlementResponse{}, err
	}

	giftOrderTotal := grossSpend
	reconciliationStatus := "PASS"
	mismatchCount := 0
	if walletDebitTotal != giftOrderTotal {
		reconciliationStatus = "FAIL"
		mismatchCount = 1
	}
	detailsObj := map[string]any{
		"wallet_gift_debit_total": walletDebitTotal,
		"gift_order_coin_total":   giftOrderTotal,
	}
	detailsBytes, err := json.Marshal(detailsObj)
	if err != nil {
		return SettlementResponse{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reconciliation_results (
			live_session_id, status, wallet_gift_debit_total, gift_order_coin_total, mismatch_count, details_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			wallet_gift_debit_total = VALUES(wallet_gift_debit_total),
			gift_order_coin_total = VALUES(gift_order_coin_total),
			mismatch_count = VALUES(mismatch_count),
			details_json = VALUES(details_json),
			generated_at = VALUES(generated_at)
	`, liveSessionID, reconciliationStatus, walletDebitTotal, giftOrderTotal, mismatchCount, string(detailsBytes), time.Now().UTC()); err != nil {
		return SettlementResponse{}, err
	}

	if err := tx.Commit(); err != nil {
		return SettlementResponse{}, err
	}

	res, appErr, err := s.GetSettlement(ctx, liveSessionID)
	if err != nil {
		return SettlementResponse{}, err
	}
	if appErr != nil {
		return SettlementResponse{}, fmt.Errorf("get settlement app error: %s", appErr.Code)
	}
	return res, nil
}

// GetSettlement returns settlement and reconciliation rows plus top contributors.
func (s *Service) GetSettlement(ctx context.Context, liveSessionID uint64) (SettlementResponse, *AppError, error) {
	resp := SettlementResponse{LiveSessionID: liveSessionID}
	var details string
	err := s.db.QueryRowContext(ctx, `
		SELECT ss.gross_coin_spend, ss.accepted_gift_count, ss.diamond_reward_total,
		       rr.status, rr.wallet_gift_debit_total, rr.gift_order_coin_total,
		       rr.mismatch_count, CAST(rr.details_json AS CHAR)
		FROM stream_settlements ss
		JOIN reconciliation_results rr ON rr.live_session_id = ss.live_session_id
		WHERE ss.live_session_id = ?
	`, liveSessionID).Scan(
		&resp.GrossCoinSpend,
		&resp.AcceptedGiftCount,
		&resp.DiamondRewardTotal,
		&resp.ReconciliationStatus,
		&resp.WalletGiftDebitTotal,
		&resp.GiftOrderCoinTotal,
		&resp.MismatchCount,
		&details,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SettlementResponse{}, &AppError{HTTPStatus: 404, Code: "SETTLEMENT_NOT_FOUND", Message: "settlement not found"}, nil
	}
	if err != nil {
		return SettlementResponse{}, nil, err
	}
	resp.ReconciliationDetails = details

	rows, err := s.db.QueryContext(ctx, `
		SELECT viewer_id, SUM(charged_coins) AS total
		FROM gift_orders
		WHERE live_session_id = ? AND status = 'ACCEPTED'
		GROUP BY viewer_id
		ORDER BY total DESC, viewer_id ASC
		LIMIT 20
	`, liveSessionID)
	if err != nil {
		return SettlementResponse{}, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var c ContributorSummary
		if err := rows.Scan(&c.ViewerID, &c.Coins); err != nil {
			return SettlementResponse{}, nil, err
		}
		resp.TopContributors = append(resp.TopContributors, c)
	}
	if err := rows.Err(); err != nil {
		return SettlementResponse{}, nil, err
	}
	return resp, nil, nil
}
