package integration

import (
	"context"
	"os"
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/mini_station/live-revenue-integrity-lab/internal/projector/leaderboard"
	"github.com/mini_station/live-revenue-integrity-lab/internal/projector/points"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
	"github.com/redis/go-redis/v9"
)

func TestLeaderboardProjectorRestartNoLoss(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() {
		_ = rdb.Close()
	}()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", redisAddr, err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}

	svc := service.New(db)
	gift1, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "lb-restart-1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		GiftID:        "ROSE",
		Quantity:      2,
		MatchID:       uint64Ptr(8001),
		SentAtMS:      1,
	})
	if err != nil || appErr != nil {
		t.Fatalf("send gift1 failed err=%v appErr=%#v", err, appErr)
	}
	gift2, appErr, err := svc.SendGift(ctx, service.GiftRequest{
		RequestID:     "lb-restart-2",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		GiftID:        "HEART",
		Quantity:      1,
		MatchID:       uint64Ptr(8001),
		SentAtMS:      2,
	})
	if err != nil || appErr != nil {
		t.Fatalf("send gift2 failed err=%v appErr=%#v", err, appErr)
	}

	campaignID := uint64(7001)
	e1 := events.LiveGiftAcceptedV1{GiftOrderID: gift1.GiftOrderID, ViewerID: 2001, CreatorID: 1001, LiveSessionID: 9001, CampaignID: &campaignID, MatchID: uint64Ptr(8001), GiftID: "ROSE", Quantity: 2, ChargedCoins: gift1.ChargedCoins, MatchPoints: gift1.MatchPointsAdded, DiamondReward: gift1.DiamondReward, RequestID: "lb-restart-1"}
	e2 := events.LiveGiftAcceptedV1{GiftOrderID: gift2.GiftOrderID, ViewerID: 2001, CreatorID: 1001, LiveSessionID: 9001, CampaignID: &campaignID, MatchID: uint64Ptr(8001), GiftID: "HEART", Quantity: 1, ChargedCoins: gift2.ChargedCoins, MatchPoints: gift2.MatchPointsAdded, DiamondReward: gift2.DiamondReward, RequestID: "lb-restart-2"}

	p1 := leaderboard.New(db, rdb)
	if err := p1.ApplyAcceptedGift(ctx, 90001, e1); err != nil {
		t.Fatalf("apply first event: %v", err)
	}

	p2 := leaderboard.New(db, rdb)
	if err := p2.ApplyAcceptedGift(ctx, 90002, e2); err != nil {
		t.Fatalf("apply second event after restart: %v", err)
	}

	assertZScore(ctx, t, rdb, "lb:contributors:9001", "2001", float64(gift1.ChargedCoins+gift2.ChargedCoins))
}

func TestPointsProjectorRestartNoLoss(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	p1 := points.New(db)
	p2 := points.New(db)

	evt := events.LiveCommentCreatedV1{EventID: "restart-comment", ViewerID: 2001, CreatorID: 1001, LiveSessionID: 9001}
	for i := uint64(0); i < 10; i++ {
		if err := p1.ApplyComment(ctx, 99000+i, evt); err != nil {
			t.Fatalf("apply p1 comment %d: %v", i, err)
		}
	}
	for i := uint64(10); i < 25; i++ {
		if err := p2.ApplyComment(ctx, 99000+i, evt); err != nil {
			t.Fatalf("apply p2 comment %d: %v", i, err)
		}
	}

	var total int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(points),0)
		FROM fan_point_ledger
		WHERE viewer_id = 2001 AND live_session_id = 9001 AND reason = 'COMMENT'
	`).Scan(&total); err != nil {
		t.Fatalf("query total fan comment points: %v", err)
	}
	if total != 20 {
		t.Fatalf("expected cap 20 after restart processing, got %d", total)
	}
}
