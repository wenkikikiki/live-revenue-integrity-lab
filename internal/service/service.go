package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
)

const (
	walletCurrencyCoin = "COIN"
	txTypeRecharge     = "RECHARGE"

	walletTopic = "wallet.transaction.posted.v1"
)

// Service is the transactional domain service layer.
type Service struct {
	db *sql.DB
}

// New creates a domain service.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// RechargeRequest is the external recharge contract.
type RechargeRequest struct {
	RequestID  string `json:"request_id"`
	ViewerID   uint64 `json:"viewer_id"`
	Coins      uint32 `json:"coins"`
	PaymentRef string `json:"payment_ref"`
}

// RechargeResponse is returned by recharge API.
type RechargeResponse struct {
	TxID        uint64 `json:"tx_id"`
	ViewerID    uint64 `json:"viewer_id"`
	CoinsAdded  uint32 `json:"coins_added"`
	NewBalance  int64  `json:"new_balance"`
	Idempotency string `json:"idempotency"`
}

// LedgerEntry is a signed posting in the double-entry ledger.
type LedgerEntry struct {
	AccountCode string
	Amount      int64
}

// BuildRechargeEntries derives the recharge postings.
func BuildRechargeEntries(viewerID uint64, coins uint32) []LedgerEntry {
	amount := int64(coins)
	return []LedgerEntry{
		{AccountCode: fmt.Sprintf("USER_COINS:%d", viewerID), Amount: amount},
		{AccountCode: "SYSTEM_TOPUP_SOURCE", Amount: -amount},
	}
}

// SumLedgerEntries returns the signed amount sum.
func SumLedgerEntries(entries []LedgerEntry) int64 {
	var sum int64
	for _, e := range entries {
		sum += e.Amount
	}
	return sum
}

// BodyHashHex hashes a payload with deterministic JSON serialization.
func BodyHashHex(v any) (string, []byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), h[:], nil
}

// Recharge posts a balanced recharge transaction with request idempotency.
func (s *Service) Recharge(ctx context.Context, req RechargeRequest) (RechargeResponse, *AppError, error) {
	if strings.TrimSpace(req.RequestID) == "" || req.ViewerID == 0 || strings.TrimSpace(req.PaymentRef) == "" {
		return RechargeResponse{}, errValidation, nil
	}
	if req.Coins < 1 || req.Coins > 100000 {
		return RechargeResponse{}, &AppError{HTTPStatus: 400, Code: "VALIDATION_ERROR", Message: "coins must be in [1,100000]"}, nil
	}

	_, bodyHash, err := BodyHashHex(req)
	if err != nil {
		return RechargeResponse{}, nil, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RechargeResponse{}, nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var existing struct {
		BodyHash         []byte
		Coins            uint32
		ResultingBalance int64
		TxID             uint64
	}
	err = tx.QueryRowContext(
		ctx,
		`SELECT body_hash, coins, resulting_balance, tx_id
		 FROM recharge_requests
		 WHERE viewer_id = ? AND request_id = ?`,
		req.ViewerID,
		req.RequestID,
	).Scan(&existing.BodyHash, &existing.Coins, &existing.ResultingBalance, &existing.TxID)
	if err == nil {
		if !bytesEqual(existing.BodyHash, bodyHash) {
			return RechargeResponse{}, &AppError{HTTPStatus: 409, Code: "IDEMPOTENCY_PAYLOAD_MISMATCH", Message: "same request_id with different payload"}, nil
		}
		if err := tx.Commit(); err != nil {
			return RechargeResponse{}, nil, err
		}
		return RechargeResponse{
			TxID:        existing.TxID,
			ViewerID:    req.ViewerID,
			CoinsAdded:  existing.Coins,
			NewBalance:  existing.ResultingBalance,
			Idempotency: "replayed",
		}, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RechargeResponse{}, nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_accounts (user_id, currency, available_balance)
		VALUES (?, 'COIN', 0)
		ON DUPLICATE KEY UPDATE user_id = VALUES(user_id)
	`, req.ViewerID); err != nil {
		return RechargeResponse{}, nil, err
	}

	var currentBalance int64
	if err := tx.QueryRowContext(ctx, `
		SELECT available_balance
		FROM wallet_accounts
		WHERE user_id = ? AND currency = 'COIN'
		FOR UPDATE
	`, req.ViewerID).Scan(&currentBalance); err != nil {
		return RechargeResponse{}, nil, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (tx_type, actor_user_id, request_id, body_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, txTypeRecharge, req.ViewerID, req.RequestID, bodyHash, time.Now().UTC())
	if err != nil {
		return RechargeResponse{}, nil, err
	}

	txID64, err := res.LastInsertId()
	if err != nil {
		return RechargeResponse{}, nil, err
	}
	if txID64 < 1 {
		return RechargeResponse{}, nil, fmt.Errorf("invalid tx id generated: %d", txID64)
	}
	txID := uint64(txID64)

	entries := BuildRechargeEntries(req.ViewerID, req.Coins)
	if SumLedgerEntries(entries) != 0 {
		return RechargeResponse{}, nil, fmt.Errorf("ledger imbalance: recharge entries do not sum to zero")
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO wallet_entries (tx_id, account_code, amount) VALUES (?, ?, ?)`,
			txID,
			e.AccountCode,
			e.Amount,
		); err != nil {
			return RechargeResponse{}, nil, err
		}
	}

	newBalance := currentBalance + int64(req.Coins)
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_accounts
		SET available_balance = ?
		WHERE user_id = ? AND currency = 'COIN'
	`, newBalance, req.ViewerID); err != nil {
		return RechargeResponse{}, nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO recharge_requests (
			viewer_id, request_id, body_hash, coins, payment_ref, resulting_balance, tx_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, req.ViewerID, req.RequestID, bodyHash, req.Coins, req.PaymentRef, newBalance, txID, time.Now().UTC()); err != nil {
		return RechargeResponse{}, nil, err
	}

	payload, err := events.MarshalJSONString(events.WalletTransactionPostedV1{
		TxID:         txID,
		TxType:       txTypeRecharge,
		ActorUserID:  req.ViewerID,
		RequestID:    req.RequestID,
		DeltaCoins:   int64(req.Coins),
		BalanceAfter: newBalance,
	})
	if err != nil {
		return RechargeResponse{}, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, walletTopic, fmt.Sprintf("%d", req.ViewerID), payload, time.Now().UTC()); err != nil {
		return RechargeResponse{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return RechargeResponse{}, nil, err
	}
	return RechargeResponse{
		TxID:        txID,
		ViewerID:    req.ViewerID,
		CoinsAdded:  req.Coins,
		NewBalance:  newBalance,
		Idempotency: "created",
	}, nil, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
