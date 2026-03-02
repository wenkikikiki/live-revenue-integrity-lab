package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
)

func TestSettlementGenerationAndReconciliationPass(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	svc := service.New(db)

	for _, req := range []service.GiftRequest{
		{RequestID: "settle-1", ViewerID: 2001, CreatorID: 1001, LiveSessionID: 9001, GiftID: "HEART", Quantity: 2, SentAtMS: 1},
		{RequestID: "settle-2", ViewerID: 2002, CreatorID: 1001, LiveSessionID: 9001, GiftID: "ROSE", Quantity: 3, SentAtMS: 2},
	} {
		if _, appErr, err := svc.SendGift(ctx, req); err != nil || appErr != nil {
			t.Fatalf("send gift failed req=%s err=%v appErr=%#v", req.RequestID, err, appErr)
		}
	}

	if appErr := svc.CloseLiveSession(ctx, 9001); appErr != nil {
		t.Fatalf("close live failed: %#v", appErr)
	}

	settlement, err := svc.GenerateSettlement(ctx, 9001)
	if err != nil {
		t.Fatalf("generate settlement: %v", err)
	}
	if settlement.ReconciliationStatus != "PASS" {
		t.Fatalf("expected PASS reconciliation, got %s", settlement.ReconciliationStatus)
	}
	if settlement.GrossCoinSpend != 13 || settlement.AcceptedGiftCount != 2 || settlement.DiamondRewardTotal != 13 {
		t.Fatalf("unexpected settlement totals: %+v", settlement)
	}

	var closedEventCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM outbox_events
		WHERE topic = 'live.session.closed.v1' AND event_key = '9001'
	`).Scan(&closedEventCount); err != nil {
		t.Fatalf("query close outbox: %v", err)
	}
	if closedEventCount != 1 {
		t.Fatalf("expected one close event outbox row, got %d", closedEventCount)
	}

	_, appErr, err := svc.GetSettlement(ctx, 9001)
	if err != nil {
		t.Fatalf("get settlement err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("get settlement app err: %#v", appErr)
	}
}

func TestSettlementCorruptionProducesFail(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	svc := service.New(db)
	if _, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "settle-corrupt-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		GiftID:        "HEART",
		Quantity:      2,
		SentAtMS:      1,
	}); err != nil || appErr != nil {
		t.Fatalf("send gift failed err=%v appErr=%#v", err, appErr)
	}

	var txID uint64
	if err := db.QueryRowContext(ctx, `
		SELECT wt.tx_id
		FROM wallet_transactions wt
		WHERE wt.actor_user_id = 2001 AND wt.request_id = 'settle-corrupt-1' AND wt.tx_type = 'GIFT_DEBIT'
	`).Scan(&txID); err != nil {
		t.Fatalf("lookup tx id: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE wallet_entries
		SET amount = amount - 1
		WHERE tx_id = ? AND account_code LIKE 'USER_COINS:%'
	`, txID); err != nil {
		t.Fatalf("seed corruption: %v", err)
	}

	settlement, err := svc.GenerateSettlement(ctx, 9001)
	if err != nil {
		t.Fatalf("generate settlement: %v", err)
	}
	if settlement.ReconciliationStatus != "FAIL" {
		t.Fatalf("expected FAIL reconciliation, got %s", settlement.ReconciliationStatus)
	}
	if settlement.MismatchCount == 0 {
		t.Fatalf("expected mismatch_count > 0")
	}
	if !strings.Contains(settlement.ReconciliationDetails, "wallet_gift_debit_total") || !strings.Contains(settlement.ReconciliationDetails, "gift_order_coin_total") {
		t.Fatalf("expected reconciliation details to name mismatched totals, got: %s", settlement.ReconciliationDetails)
	}
}
