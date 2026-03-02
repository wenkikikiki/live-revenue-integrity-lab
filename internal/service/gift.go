package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	mysqlerr "github.com/go-sql-driver/mysql"
	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
)

const (
	txTypeGiftDebit = "GIFT_DEBIT"

	giftAcceptedTopic = "live.gift.accepted.v1"
	giftRejectedTopic = "live.gift.rejected.v1"
)

// GiftRequest is the external send-gift contract.
type GiftRequest struct {
	RequestID     string  `json:"request_id"`
	ViewerID      uint64  `json:"viewer_id"`
	CreatorID     uint64  `json:"creator_id"`
	LiveSessionID uint64  `json:"live_session_id"`
	MatchID       *uint64 `json:"match_id"`
	GiftID        string  `json:"gift_id"`
	Quantity      uint16  `json:"quantity"`
	SentAtMS      int64   `json:"sent_at_ms"`
}

// GiftResponse is returned by POST /v1/gifts.
type GiftResponse struct {
	GiftOrderID      uint64 `json:"gift_order_id"`
	Accepted         bool   `json:"accepted"`
	ChargedCoins     uint32 `json:"charged_coins"`
	MatchPointsAdded uint32 `json:"match_points_added"`
	DiamondReward    uint32 `json:"diamond_reward"`
	NewBalance       int64  `json:"new_balance"`
	Idempotency      string `json:"idempotency"`
}

type matchData struct {
	MatchID        uint64
	LiveSessionID  uint64
	Mode           string
	SpecificGiftID sql.NullString
	Status         string
}

// SendGift validates gifting policy, posts the debit, and writes outbox events.
func (s *Service) SendGift(ctx context.Context, req GiftRequest) (GiftResponse, *AppError, error) {
	if strings.TrimSpace(req.RequestID) == "" || req.ViewerID == 0 || req.CreatorID == 0 || req.LiveSessionID == 0 || strings.TrimSpace(req.GiftID) == "" {
		return GiftResponse{}, errValidation, nil
	}
	if req.Quantity < 1 || req.Quantity > 99 {
		return GiftResponse{}, &AppError{HTTPStatus: 400, Code: "VALIDATION_ERROR", Message: "quantity must be in [1,99]"}, nil
	}
	if req.ViewerID == req.CreatorID {
		return GiftResponse{}, &AppError{HTTPStatus: 400, Code: "VALIDATION_ERROR", Message: "viewer and creator cannot be the same"}, nil
	}

	_, bodyHash, err := BodyHashHex(req)
	if err != nil {
		return GiftResponse{}, nil, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return GiftResponse{}, nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	replayed, appErr, err := s.replayGiftIfExists(ctx, tx, req, bodyHash)
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if appErr != nil {
		return GiftResponse{}, appErr, nil
	}
	if replayed != nil {
		if err := tx.Commit(); err != nil {
			return GiftResponse{}, nil, err
		}
		return *replayed, nil, nil
	}

	var liveStatus string
	var campaignID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT status, campaign_id
		FROM live_sessions
		WHERE live_session_id = ?
	`, req.LiveSessionID).Scan(&liveStatus, &campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return s.rejectGift(ctx, tx, req, bodyHash, "LIVE_SESSION_NOT_FOUND", 404)
	}
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if liveStatus != "OPEN" {
		return s.rejectGift(ctx, tx, req, bodyHash, "LIVE_SESSION_CLOSED", 409)
	}

	var giftPrice, giftMatchPoints, giftDiamond int64
	var giftEnabled bool
	err = tx.QueryRowContext(ctx, `
		SELECT coin_price, match_points, diamond_reward, enabled
		FROM gift_catalog
		WHERE gift_id = ?
	`, req.GiftID).Scan(&giftPrice, &giftMatchPoints, &giftDiamond, &giftEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return s.rejectGift(ctx, tx, req, bodyHash, "GIFT_NOT_FOUND", 404)
	}
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if !giftEnabled {
		return s.rejectGift(ctx, tx, req, bodyHash, "GIFT_NOT_FOUND", 404)
	}

	eligible, err := s.isCreatorEligible(ctx, tx, req.CreatorID)
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if !eligible {
		return s.rejectGift(ctx, tx, req, bodyHash, "CREATOR_NOT_ELIGIBLE", 403)
	}

	var match *matchData
	if req.MatchID != nil {
		match = &matchData{}
		err = tx.QueryRowContext(ctx, `
			SELECT match_id, live_session_id, mode, specific_gift_id, status
			FROM live_matches
			WHERE match_id = ?
		`, *req.MatchID).Scan(&match.MatchID, &match.LiveSessionID, &match.Mode, &match.SpecificGiftID, &match.Status)
		if errors.Is(err, sql.ErrNoRows) {
			return s.rejectGift(ctx, tx, req, bodyHash, "MATCH_CLOSED", 409)
		}
		if err != nil {
			return GiftResponse{}, nil, err
		}
		if match.LiveSessionID != req.LiveSessionID || match.Status != "OPEN" {
			return s.rejectGift(ctx, tx, req, bodyHash, "MATCH_CLOSED", 409)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_accounts (user_id, currency, available_balance)
		VALUES (?, 'COIN', 0)
		ON DUPLICATE KEY UPDATE user_id = VALUES(user_id)
	`, req.ViewerID); err != nil {
		return GiftResponse{}, nil, err
	}

	var balance int64
	if err := tx.QueryRowContext(ctx, `
		SELECT available_balance
		FROM wallet_accounts
		WHERE user_id = ? AND currency = 'COIN'
		FOR UPDATE
	`, req.ViewerID).Scan(&balance); err != nil {
		return GiftResponse{}, nil, err
	}

	charged := giftPrice * int64(req.Quantity)
	if charged <= 0 {
		return GiftResponse{}, nil, fmt.Errorf("invalid charged coins: %d", charged)
	}
	chargedCoinsU32, convErr := int64ToUint32(charged, "charged coins")
	if convErr != nil {
		return GiftResponse{}, nil, convErr
	}
	if balance < charged {
		return s.rejectGift(ctx, tx, req, bodyHash, "INSUFFICIENT_BALANCE", 409)
	}

	matchPoints := giftMatchPoints * int64(req.Quantity)
	if match != nil && match.Mode == "SPECIFIC_GIFT" {
		if !match.SpecificGiftID.Valid || !strings.EqualFold(match.SpecificGiftID.String, req.GiftID) {
			matchPoints = 0
		}
	}
	matchPointsU32, convErr := int64ToUint32(matchPoints, "match points")
	if convErr != nil {
		return GiftResponse{}, nil, convErr
	}
	diamondReward := giftDiamond * int64(req.Quantity)
	diamondRewardU32, convErr := int64ToUint32(diamondReward, "diamond reward")
	if convErr != nil {
		return GiftResponse{}, nil, convErr
	}

	walletTxRes, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_transactions (tx_type, actor_user_id, request_id, body_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, txTypeGiftDebit, req.ViewerID, req.RequestID, bodyHash, time.Now().UTC())
	if err != nil {
		if isDupKey(err) {
			replayed, appErr, replayErr := s.replayGiftIfExists(ctx, tx, req, bodyHash)
			if replayErr != nil {
				return GiftResponse{}, nil, replayErr
			}
			if appErr != nil {
				return GiftResponse{}, appErr, nil
			}
			if replayed != nil {
				if err := tx.Commit(); err != nil {
					return GiftResponse{}, nil, err
				}
				return *replayed, nil, nil
			}
		}
		return GiftResponse{}, nil, err
	}
	walletTxID64, err := walletTxRes.LastInsertId()
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if walletTxID64 < 1 {
		return GiftResponse{}, nil, fmt.Errorf("invalid wallet tx id: %d", walletTxID64)
	}
	walletTxID := uint64(walletTxID64)

	entries := []LedgerEntry{
		{AccountCode: fmt.Sprintf("USER_COINS:%d", req.ViewerID), Amount: -charged},
		{AccountCode: "PLATFORM_RESERVED_COINS", Amount: charged},
	}
	if SumLedgerEntries(entries) != 0 {
		return GiftResponse{}, nil, fmt.Errorf("ledger imbalance for gift")
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wallet_entries (tx_id, account_code, amount) VALUES (?, ?, ?)`, walletTxID, e.AccountCode, e.Amount); err != nil {
			return GiftResponse{}, nil, err
		}
	}

	newBalance := balance - charged
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallet_accounts
		SET available_balance = ?
		WHERE user_id = ? AND currency = 'COIN'
	`, newBalance, req.ViewerID); err != nil {
		return GiftResponse{}, nil, err
	}

	giftOrderRes, err := tx.ExecContext(ctx, `
		INSERT INTO gift_orders (
			viewer_id, creator_id, live_session_id, match_id, gift_id, quantity,
			charged_coins, match_points_added, diamond_reward, post_balance,
			status, reject_code, request_id, body_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ACCEPTED', NULL, ?, ?, ?)
	`, req.ViewerID, req.CreatorID, req.LiveSessionID, nullableUint64(req.MatchID), req.GiftID, req.Quantity,
		charged, matchPoints, diamondReward, newBalance,
		req.RequestID, bodyHash, time.Now().UTC())
	if err != nil {
		if isDupKey(err) {
			replayed, appErr, replayErr := s.replayGiftIfExists(ctx, tx, req, bodyHash)
			if replayErr != nil {
				return GiftResponse{}, nil, replayErr
			}
			if appErr != nil {
				return GiftResponse{}, appErr, nil
			}
			if replayed != nil {
				if err := tx.Commit(); err != nil {
					return GiftResponse{}, nil, err
				}
				return *replayed, nil, nil
			}
		}
		return GiftResponse{}, nil, err
	}
	giftOrderID64, err := giftOrderRes.LastInsertId()
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if giftOrderID64 < 1 {
		return GiftResponse{}, nil, fmt.Errorf("invalid gift order id: %d", giftOrderID64)
	}
	giftOrderID := uint64(giftOrderID64)

	walletPayload, err := events.MarshalJSONString(events.WalletTransactionPostedV1{
		TxID:         walletTxID,
		TxType:       txTypeGiftDebit,
		ActorUserID:  req.ViewerID,
		RequestID:    req.RequestID,
		DeltaCoins:   -charged,
		BalanceAfter: newBalance,
	})
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, walletTopic, fmt.Sprintf("%d", req.ViewerID), walletPayload, time.Now().UTC()); err != nil {
		return GiftResponse{}, nil, err
	}

	var campaignIDPtr *uint64
	if campaignID.Valid {
		v, convErr := int64ToUint64(campaignID.Int64, "campaign_id")
		if convErr != nil {
			return GiftResponse{}, nil, convErr
		}
		campaignIDPtr = &v
	}
	acceptedPayload, err := events.MarshalJSONString(events.LiveGiftAcceptedV1{
		EventType:     giftAcceptedTopic,
		GiftOrderID:   giftOrderID,
		ViewerID:      req.ViewerID,
		CreatorID:     req.CreatorID,
		LiveSessionID: req.LiveSessionID,
		CampaignID:    campaignIDPtr,
		MatchID:       req.MatchID,
		GiftID:        req.GiftID,
		Quantity:      req.Quantity,
		ChargedCoins:  chargedCoinsU32,
		MatchPoints:   matchPointsU32,
		DiamondReward: diamondRewardU32,
		RequestID:     req.RequestID,
	})
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, giftAcceptedTopic, fmt.Sprintf("%d", req.LiveSessionID), acceptedPayload, time.Now().UTC()); err != nil {
		return GiftResponse{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return GiftResponse{}, nil, err
	}
	return GiftResponse{
		GiftOrderID:      giftOrderID,
		Accepted:         true,
		ChargedCoins:     chargedCoinsU32,
		MatchPointsAdded: matchPointsU32,
		DiamondReward:    diamondRewardU32,
		NewBalance:       newBalance,
		Idempotency:      "created",
	}, nil, nil
}

func (s *Service) replayGiftIfExists(ctx context.Context, tx *sql.Tx, req GiftRequest, bodyHash []byte) (*GiftResponse, *AppError, error) {
	var (
		giftOrderID      uint64
		existingHash     []byte
		status           string
		rejectCode       sql.NullString
		chargedCoins     uint32
		matchPointsAdded uint32
		diamondReward    uint32
		postBalance      sql.NullInt64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT gift_order_id, body_hash, status, reject_code, charged_coins, match_points_added, diamond_reward, post_balance
		FROM gift_orders
		WHERE viewer_id = ? AND request_id = ?
		FOR UPDATE
	`, req.ViewerID, req.RequestID).Scan(
		&giftOrderID,
		&existingHash,
		&status,
		&rejectCode,
		&chargedCoins,
		&matchPointsAdded,
		&diamondReward,
		&postBalance,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !bytesEqual(existingHash, bodyHash) {
		return nil, &AppError{HTTPStatus: 409, Code: "IDEMPOTENCY_PAYLOAD_MISMATCH", Message: "same request_id with different payload"}, nil
	}
	if status == "ACCEPTED" {
		balance := int64(0)
		if postBalance.Valid {
			balance = postBalance.Int64
		}
		return &GiftResponse{
			GiftOrderID:      giftOrderID,
			Accepted:         true,
			ChargedCoins:     chargedCoins,
			MatchPointsAdded: matchPointsAdded,
			DiamondReward:    diamondReward,
			NewBalance:       balance,
			Idempotency:      "replayed",
		}, nil, nil
	}
	code := "REJECTED"
	if rejectCode.Valid {
		code = rejectCode.String
	}
	return nil, mapRejectCodeToAppError(code), nil
}

func (s *Service) rejectGift(ctx context.Context, tx *sql.Tx, req GiftRequest, bodyHash []byte, rejectCode string, httpStatus int) (GiftResponse, *AppError, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO gift_orders (
			viewer_id, creator_id, live_session_id, match_id, gift_id, quantity,
			charged_coins, match_points_added, diamond_reward, post_balance,
			status, reject_code, request_id, body_hash, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, NULL, 'REJECTED', ?, ?, ?, ?)
	`, req.ViewerID, req.CreatorID, req.LiveSessionID, nullableUint64(req.MatchID), req.GiftID, req.Quantity, rejectCode, req.RequestID, bodyHash, time.Now().UTC()); err != nil {
		if isDupKey(err) {
			replayed, appErr, replayErr := s.replayGiftIfExists(ctx, tx, req, bodyHash)
			if replayErr != nil {
				return GiftResponse{}, nil, replayErr
			}
			if replayed != nil {
				return *replayed, nil, nil
			}
			if appErr != nil {
				if commitErr := tx.Commit(); commitErr != nil {
					return GiftResponse{}, nil, commitErr
				}
				return GiftResponse{}, appErr, nil
			}
		}
		return GiftResponse{}, nil, err
	}

	rejectedPayload, err := events.MarshalJSONString(events.LiveGiftRejectedV1{
		EventType:     giftRejectedTopic,
		ViewerID:      req.ViewerID,
		CreatorID:     req.CreatorID,
		LiveSessionID: req.LiveSessionID,
		MatchID:       req.MatchID,
		GiftID:        req.GiftID,
		Quantity:      req.Quantity,
		RequestID:     req.RequestID,
		RejectCode:    rejectCode,
	})
	if err != nil {
		return GiftResponse{}, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES (?, ?, ?, ?)
	`, giftRejectedTopic, fmt.Sprintf("%d", req.LiveSessionID), rejectedPayload, time.Now().UTC()); err != nil {
		return GiftResponse{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return GiftResponse{}, nil, err
	}
	return GiftResponse{}, &AppError{HTTPStatus: httpStatus, Code: rejectCode, Message: strings.ToLower(strings.ReplaceAll(rejectCode, "_", " "))}, nil
}

func (s *Service) isCreatorEligible(ctx context.Context, tx *sql.Tx, creatorID uint64) (bool, error) {
	var (
		ageYears        uint8
		regionCode      string
		accountStanding string
		canGoLive       bool
		giftsEnabled    bool
		accountType     string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT age_years, region_code, account_standing, can_go_live, live_gifts_enabled, account_type
		FROM users
		WHERE user_id = ?
	`, creatorID).Scan(&ageYears, &regionCode, &accountStanding, &canGoLive, &giftsEnabled, &accountType)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if accountStanding != "GOOD" || !canGoLive || !giftsEnabled {
		return false, nil
	}
	if accountType != "NORMAL" {
		return false, nil
	}
	if strings.EqualFold(regionCode, "KR") {
		return ageYears >= 19, nil
	}
	return ageYears >= 18, nil
}

func mapRejectCodeToAppError(code string) *AppError {
	switch code {
	case "CREATOR_NOT_ELIGIBLE":
		return &AppError{HTTPStatus: 403, Code: code, Message: "creator is not eligible"}
	case "LIVE_SESSION_NOT_FOUND":
		return &AppError{HTTPStatus: 404, Code: code, Message: "live session not found"}
	case "GIFT_NOT_FOUND":
		return &AppError{HTTPStatus: 404, Code: code, Message: "gift not found"}
	case "LIVE_SESSION_CLOSED":
		return &AppError{HTTPStatus: 409, Code: code, Message: "live session closed"}
	case "MATCH_CLOSED":
		return &AppError{HTTPStatus: 409, Code: code, Message: "match closed"}
	case "INSUFFICIENT_BALANCE":
		return &AppError{HTTPStatus: 409, Code: code, Message: "insufficient balance"}
	default:
		return &AppError{HTTPStatus: 409, Code: code, Message: "gift rejected"}
	}
}

func isDupKey(err error) bool {
	var myErr *mysqlerr.MySQLError
	if !errors.As(err, &myErr) {
		return false
	}
	return myErr.Number == 1062
}

func nullableUint64(v *uint64) any {
	if v == nil {
		return nil
	}
	return *v
}

func int64ToUint32(v int64, name string) (uint32, error) {
	if v < 0 || v > math.MaxUint32 {
		return 0, fmt.Errorf("%s overflow uint32: %d", name, v)
	}
	return uint32(v), nil
}

func int64ToUint64(v int64, name string) (uint64, error) {
	if v < 0 {
		return 0, fmt.Errorf("%s overflow uint64: %d", name, v)
	}
	return uint64(v), nil
}
