package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/mini_station/live-revenue-integrity-lab/internal/projector/leaderboard"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
	"github.com/redis/go-redis/v9"
)

func TestLeaderboardProjectorAndRebuild(t *testing.T) {
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
	projector := leaderboard.New(db, rdb)

	sendAccepted := func(req service.GiftRequest) (service.GiftResponse, events.LiveGiftAcceptedV1) {
		res, appErr, err := svc.SendGift(ctx, req)
		if err != nil {
			t.Fatalf("send gift err (%s): %v", req.RequestID, err)
		}
		if appErr != nil {
			t.Fatalf("send gift app err (%s): %#v", req.RequestID, appErr)
		}

		var campaignID *uint64
		if req.LiveSessionID == 9001 {
			v := uint64(7001)
			campaignID = &v
		}
		evt := events.LiveGiftAcceptedV1{
			EventType:     "live.gift.accepted.v1",
			GiftOrderID:   res.GiftOrderID,
			ViewerID:      req.ViewerID,
			CreatorID:     req.CreatorID,
			LiveSessionID: req.LiveSessionID,
			CampaignID:    campaignID,
			MatchID:       req.MatchID,
			GiftID:        req.GiftID,
			Quantity:      req.Quantity,
			ChargedCoins:  res.ChargedCoins,
			MatchPoints:   res.MatchPointsAdded,
			DiamondReward: res.DiamondReward,
			RequestID:     req.RequestID,
		}
		return res, evt
	}

	_, e1 := sendAccepted(service.GiftRequest{
		RequestID:     "lb-g1",
		ViewerID:      2001,
		CreatorID:     1001,
		LiveSessionID: 9001,
		MatchID:       uint64Ptr(8001),
		GiftID:        "HEART",
		Quantity:      2,
		SentAtMS:      1,
	})
	_, e2 := sendAccepted(service.GiftRequest{
		RequestID:     "lb-g2",
		ViewerID:      2002,
		CreatorID:     1001,
		LiveSessionID: 9001,
		MatchID:       uint64Ptr(8001),
		GiftID:        "ROSE",
		Quantity:      3,
		SentAtMS:      2,
	})
	_, e3 := sendAccepted(service.GiftRequest{
		RequestID:     "lb-g3",
		ViewerID:      2002,
		CreatorID:     1002,
		LiveSessionID: 9002,
		MatchID:       uint64Ptr(8002),
		GiftID:        "HEART",
		Quantity:      1,
		SentAtMS:      3,
	})
	_, e4 := sendAccepted(service.GiftRequest{
		RequestID:     "lb-g4",
		ViewerID:      2002,
		CreatorID:     1002,
		LiveSessionID: 9002,
		MatchID:       uint64Ptr(8002),
		GiftID:        "ROSE",
		Quantity:      4,
		SentAtMS:      4,
	})

	for i, evt := range []events.LiveGiftAcceptedV1{e1, e2, e3, e4} {
		if err := projector.ApplyAcceptedGift(ctx, uint64(100+i), evt); err != nil {
			t.Fatalf("apply accepted event %d: %v", i, err)
		}
	}
	if err := projector.ApplyAcceptedGift(ctx, 100, e1); err != nil {
		t.Fatalf("apply duplicate event id: %v", err)
	}

	assertZScore(ctx, t, rdb, "lb:contributors:9001", "2001", 10)
	assertZScore(ctx, t, rdb, "lb:contributors:9001", "2002", 3)
	assertHashField(ctx, t, rdb, "match:score:8001", "creator_1001_points", 13)
	assertHashField(ctx, t, rdb, "match:score:8002", "creator_1002_points", 4)
	assertZScore(ctx, t, rdb, "lb:campaign:7001", "1001", 13)

	snapshotContrib, err := rdb.ZRangeWithScores(ctx, "lb:contributors:9001", 0, -1).Result()
	if err != nil {
		t.Fatalf("snapshot contributors: %v", err)
	}
	snapshotMatch, err := rdb.HGetAll(ctx, "match:score:8001").Result()
	if err != nil {
		t.Fatalf("snapshot match: %v", err)
	}
	snapshotCampaign, err := rdb.ZRangeWithScores(ctx, "lb:campaign:7001", 0, -1).Result()
	if err != nil {
		t.Fatalf("snapshot campaign: %v", err)
	}

	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	if err := projector.RebuildLiveSession(ctx, 9001); err != nil {
		t.Fatalf("rebuild live session: %v", err)
	}

	afterContrib, err := rdb.ZRangeWithScores(ctx, "lb:contributors:9001", 0, -1).Result()
	if err != nil {
		t.Fatalf("after rebuild contributors: %v", err)
	}
	afterMatch, err := rdb.HGetAll(ctx, "match:score:8001").Result()
	if err != nil {
		t.Fatalf("after rebuild match: %v", err)
	}
	afterCampaign, err := rdb.ZRangeWithScores(ctx, "lb:campaign:7001", 0, -1).Result()
	if err != nil {
		t.Fatalf("after rebuild campaign: %v", err)
	}

	if fmt.Sprint(snapshotContrib) != fmt.Sprint(afterContrib) {
		t.Fatalf("contributors rebuild mismatch: before=%v after=%v", snapshotContrib, afterContrib)
	}
	if fmt.Sprint(snapshotMatch) != fmt.Sprint(afterMatch) {
		t.Fatalf("match rebuild mismatch: before=%v after=%v", snapshotMatch, afterMatch)
	}
	if fmt.Sprint(snapshotCampaign) != fmt.Sprint(afterCampaign) {
		t.Fatalf("campaign rebuild mismatch: before=%v after=%v", snapshotCampaign, afterCampaign)
	}
}

func assertZScore(ctx context.Context, t *testing.T, rdb *redis.Client, key, member string, want float64) {
	t.Helper()
	got, err := rdb.ZScore(ctx, key, member).Result()
	if err != nil {
		t.Fatalf("zscore %s[%s]: %v", key, member, err)
	}
	if got != want {
		t.Fatalf("zscore %s[%s]: want %.0f got %.0f", key, member, want, got)
	}
}

func assertHashField(ctx context.Context, t *testing.T, rdb *redis.Client, key, field string, want int64) {
	t.Helper()
	got, err := rdb.HGet(ctx, key, field).Int64()
	if err != nil {
		t.Fatalf("hget %s[%s]: %v", key, field, err)
	}
	if got != want {
		t.Fatalf("hget %s[%s]: want %d got %d", key, field, want, got)
	}
}
