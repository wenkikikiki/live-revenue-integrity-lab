package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
)

func TestGiftFlowIdempotencyPolicyAndConcurrency(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	svc := service.New(db)

	accepted, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-accept-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		MatchID:       uint64Ptr(8001),
		GiftID:        "HEART",
		Quantity:      10,
		SentAtMS:      1,
	})
	if err != nil {
		t.Fatalf("accepted gift err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("accepted gift app error: %#v", appErr)
	}
	if !accepted.Accepted || accepted.ChargedCoins != 50 || accepted.MatchPointsAdded != 50 || accepted.DiamondReward != 50 {
		t.Fatalf("unexpected accepted response: %+v", accepted)
	}

	replay, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-accept-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		MatchID:       uint64Ptr(8001),
		GiftID:        "HEART",
		Quantity:      10,
		SentAtMS:      1,
	})
	if err != nil {
		t.Fatalf("replay gift err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("replay gift app error: %#v", appErr)
	}
	if replay.GiftOrderID != accepted.GiftOrderID {
		t.Fatalf("expected replay gift order id %d, got %d", accepted.GiftOrderID, replay.GiftOrderID)
	}

	_, appErr, err = svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-accept-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		MatchID:       uint64Ptr(8001),
		GiftID:        "HEART",
		Quantity:      11,
		SentAtMS:      1,
	})
	if err != nil {
		t.Fatalf("idempotency mismatch err: %v", err)
	}
	if appErr == nil || appErr.Code != "IDEMPOTENCY_PAYLOAD_MISMATCH" {
		t.Fatalf("expected mismatch error, got %#v", appErr)
	}

	specific, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-specific-nonmatch",
		ViewerID:      2002,
		CreatorID:     1002,
		LiveSessionID: 9002,
		MatchID:       uint64Ptr(8002),
		GiftID:        "HEART",
		Quantity:      1,
		SentAtMS:      2,
	})
	if err != nil {
		t.Fatalf("specific nonmatch err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("specific nonmatch app err: %#v", appErr)
	}
	if specific.MatchPointsAdded != 0 {
		t.Fatalf("expected 0 match points for non-specific gift, got %d", specific.MatchPointsAdded)
	}

	_, appErr, err = svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-not-eligible",
		ViewerID:      2001,
		CreatorID:     4001,
		LiveSessionID: 9001,
		GiftID:        "ROSE",
		Quantity:      1,
		SentAtMS:      3,
	})
	if err != nil {
		t.Fatalf("creator not eligible err: %v", err)
	}
	if appErr == nil || appErr.Code != "CREATOR_NOT_ELIGIBLE" {
		t.Fatalf("expected CREATOR_NOT_ELIGIBLE, got %#v", appErr)
	}

	_, appErr, err = svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "g-insufficient",
		ViewerID:      2002,
		CreatorID:     1001,
		LiveSessionID: 9001,
		GiftID:        "LION",
		Quantity:      1,
		SentAtMS:      4,
	})
	if err != nil {
		t.Fatalf("insufficient err: %v", err)
	}
	if appErr == nil || appErr.Code != "INSUFFICIENT_BALANCE" {
		t.Fatalf("expected INSUFFICIENT_BALANCE, got %#v", appErr)
	}

	for _, reqID := range []string{"g-not-eligible", "g-insufficient"} {
		var debitCount int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM wallet_transactions
			WHERE tx_type = 'GIFT_DEBIT' AND request_id = ?
		`, reqID).Scan(&debitCount); err != nil {
			t.Fatalf("query debit count for %s: %v", reqID, err)
		}
		if debitCount != 0 {
			t.Fatalf("expected no debit transaction for rejected request %s, got %d", reqID, debitCount)
		}
	}

	var successCount int32
	var firstGiftOrder uint64
	var mu sync.Mutex
	wg := sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, appErr, err := svc.SendGift(ctx, service.GiftRequest{
				RequestID:     "g-dup-50",
				ViewerID:      2001,
				CreatorID:     1001,
				LiveSessionID: 9001,
				GiftID:        "ROSE",
				Quantity:      1,
				SentAtMS:      5,
			})
			if err != nil {
				t.Errorf("dup send gift err: %v", err)
				return
			}
			if appErr != nil {
				t.Errorf("dup send gift app err: %#v", appErr)
				return
			}
			if !res.Accepted {
				t.Errorf("dup send expected accepted response: %+v", res)
				return
			}
			mu.Lock()
			if firstGiftOrder == 0 {
				firstGiftOrder = res.GiftOrderID
			} else if firstGiftOrder != res.GiftOrderID {
				t.Errorf("expected same gift order id for duplicates, got %d and %d", firstGiftOrder, res.GiftOrderID)
			}
			mu.Unlock()
			atomic.AddInt32(&successCount, 1)
		}()
	}
	wg.Wait()

	if successCount != 50 {
		t.Fatalf("expected 50 successful duplicate responses, got %d", successCount)
	}

	var txCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM wallet_transactions
		WHERE tx_type = 'GIFT_DEBIT' AND actor_user_id = 2001 AND request_id = 'g-dup-50'
	`).Scan(&txCount); err != nil {
		t.Fatalf("query duplicate tx count: %v", err)
	}
	if txCount != 1 {
		t.Fatalf("expected exactly one debit tx for duplicate request, got %d", txCount)
	}
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}
