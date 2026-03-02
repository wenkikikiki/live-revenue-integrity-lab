package integration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/mini_station/live-revenue-integrity-lab/internal/projector/points"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
)

func TestPointsProjectorCapsAndReplay(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	svc := service.New(db)
	projector := points.New(db)

	giftRes, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "pp-gift-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		GiftID:        "HEART",
		Quantity:      4,
		SentAtMS:      1,
	})
	if err != nil {
		t.Fatalf("send gift err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("send gift app err: %#v", appErr)
	}

	campaignID := uint64(7001)
	giftEvt := events.LiveGiftAcceptedV1{
		EventType:     "live.gift.accepted.v1",
		GiftOrderID:   giftRes.GiftOrderID,
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		CampaignID:    &campaignID,
		GiftID:        "HEART",
		Quantity:      4,
		ChargedCoins:  giftRes.ChargedCoins,
		MatchPoints:   giftRes.MatchPointsAdded,
		DiamondReward: giftRes.DiamondReward,
		RequestID:     "pp-gift-1",
	}
	if err := projector.ApplyGift(ctx, 500, giftEvt); err != nil {
		t.Fatalf("apply gift event: %v", err)
	}

	commentEvt := events.LiveCommentCreatedV1{
		EventID:       "comment-e",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
	}
	for i := uint64(0); i < 25; i++ {
		if err := projector.ApplyComment(ctx, 600+i, commentEvt); err != nil {
			t.Fatalf("apply comment event %d: %v", i, err)
		}
	}

	watchEvt := events.LiveWatchMinuteV1{
		EventID:       "watch-e",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
	}
	for i := uint64(0); i < 70; i++ {
		if err := projector.ApplyWatchMinute(ctx, 700+i, watchEvt); err != nil {
			t.Fatalf("apply watch event %d: %v", i, err)
		}
	}

	assertFanPoints(ctx, t, db, "GIFT", 20)
	assertFanPoints(ctx, t, db, "COMMENT", 20)
	assertFanPoints(ctx, t, db, "WATCH_MINUTE", 60)
	assertCampaignPoints(ctx, t, db, 20)

	if err := projector.ApplyGift(ctx, 500, giftEvt); err != nil {
		t.Fatalf("replay gift event: %v", err)
	}
	for i := uint64(0); i < 25; i++ {
		if err := projector.ApplyComment(ctx, 600+i, commentEvt); err != nil {
			t.Fatalf("replay comment event %d: %v", i, err)
		}
	}
	for i := uint64(0); i < 70; i++ {
		if err := projector.ApplyWatchMinute(ctx, 700+i, watchEvt); err != nil {
			t.Fatalf("replay watch event %d: %v", i, err)
		}
	}

	assertFanPoints(ctx, t, db, "GIFT", 20)
	assertFanPoints(ctx, t, db, "COMMENT", 20)
	assertFanPoints(ctx, t, db, "WATCH_MINUTE", 60)
	assertCampaignPoints(ctx, t, db, 20)
}

func assertFanPoints(ctx context.Context, t *testing.T, db *sql.DB, reason string, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(points), 0)
		FROM fan_point_ledger
		WHERE viewer_id = 2001 AND live_session_id = 9001 AND reason = ?
	`, reason).Scan(&got); err != nil {
		t.Fatalf("query fan points for %s: %v", reason, err)
	}
	if got != want {
		t.Fatalf("fan points for %s: want %d got %d", reason, want, got)
	}
}

func assertCampaignPoints(ctx context.Context, t *testing.T, db *sql.DB, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(points), 0)
		FROM campaign_point_ledger
		WHERE campaign_id = 7001 AND creator_id = 1001
	`).Scan(&got); err != nil {
		t.Fatalf("query campaign points: %v", err)
	}
	if got != want {
		t.Fatalf("campaign points: want %d got %d", want, got)
	}
}
