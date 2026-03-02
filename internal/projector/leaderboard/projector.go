// Package leaderboard projects accepted gift events into Redis leaderboards.
package leaderboard

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	mysqlerr "github.com/go-sql-driver/mysql"
	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/redis/go-redis/v9"
)

const consumerName = "leaderboard-projector"

// Projector updates Redis leaderboard projections from accepted gift events.
type Projector struct {
	db    *sql.DB
	redis *redis.Client
}

// New creates a leaderboard projector.
func New(db *sql.DB, redisClient *redis.Client) *Projector {
	return &Projector{db: db, redis: redisClient}
}

// ApplyAcceptedGift applies one accepted gift event in an idempotent way.
func (p *Projector) ApplyAcceptedGift(ctx context.Context, eventID uint64, evt events.LiveGiftAcceptedV1) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO consumer_dedupe (consumer_name, event_id, applied_at)
		VALUES (?, ?, ?)
	`, consumerName, eventID, time.Now().UTC()); err != nil {
		if isDup(err) {
			if err := tx.Commit(); err != nil {
				return err
			}
			return nil
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	contribKey := contributorKey(evt.LiveSessionID)
	if err := p.redis.ZIncrBy(ctx, contribKey, float64(evt.ChargedCoins), strconv.FormatUint(evt.ViewerID, 10)).Err(); err != nil {
		return err
	}

	if evt.CampaignID != nil {
		campKey := campaignKey(*evt.CampaignID)
		if err := p.redis.ZIncrBy(ctx, campKey, float64(evt.ChargedCoins), strconv.FormatUint(evt.CreatorID, 10)).Err(); err != nil {
			return err
		}
	}

	if evt.MatchID != nil {
		matchKey := matchScoreKey(*evt.MatchID)
		field := fmt.Sprintf("creator_%d_points", evt.CreatorID)
		if err := p.redis.HIncrBy(ctx, matchKey, field, int64(evt.MatchPoints)).Err(); err != nil {
			return err
		}
	}

	return nil
}

// RebuildLiveSession rebuilds contributor/match/campaign projections from MySQL.
func (p *Projector) RebuildLiveSession(ctx context.Context, liveSessionID uint64) error {
	contrib := map[uint64]float64{}
	matchScores := map[uint64]map[uint64]int64{}

	rows, err := p.db.QueryContext(ctx, `
		SELECT viewer_id, creator_id, charged_coins, match_id, match_points_added
		FROM gift_orders
		WHERE live_session_id = ? AND status = 'ACCEPTED'
	`, liveSessionID)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var (
			viewerID    uint64
			creatorID   uint64
			charged     uint32
			matchID     sql.NullInt64
			matchPoints uint32
		)
		if err := rows.Scan(&viewerID, &creatorID, &charged, &matchID, &matchPoints); err != nil {
			return err
		}
		contrib[viewerID] += float64(charged)
		if matchID.Valid {
			mID, err := int64ToUint64(matchID.Int64, "match_id")
			if err != nil {
				return err
			}
			if _, ok := matchScores[mID]; !ok {
				matchScores[mID] = map[uint64]int64{}
			}
			matchScores[mID][creatorID] += int64(matchPoints)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	contribKey := contributorKey(liveSessionID)
	if err := p.redis.Del(ctx, contribKey).Err(); err != nil {
		return err
	}
	if len(contrib) > 0 {
		members := make([]redis.Z, 0, len(contrib))
		for viewerID, score := range contrib {
			members = append(members, redis.Z{Member: strconv.FormatUint(viewerID, 10), Score: score})
		}
		if err := p.redis.ZAdd(ctx, contribKey, members...).Err(); err != nil {
			return err
		}
	}

	for matchID, perCreator := range matchScores {
		mk := matchScoreKey(matchID)
		if err := p.redis.Del(ctx, mk).Err(); err != nil {
			return err
		}
		fields := map[string]any{}
		for creatorID, points := range perCreator {
			fields[fmt.Sprintf("creator_%d_points", creatorID)] = points
		}
		if len(fields) > 0 {
			if err := p.redis.HSet(ctx, mk, fields).Err(); err != nil {
				return err
			}
		}
	}

	var campaignID sql.NullInt64
	err = p.db.QueryRowContext(ctx, `SELECT campaign_id FROM live_sessions WHERE live_session_id = ?`, liveSessionID).Scan(&campaignID)
	if err != nil {
		return err
	}
	if campaignID.Valid {
		campaignIDU64, err := int64ToUint64(campaignID.Int64, "campaign_id")
		if err != nil {
			return err
		}
		campKey := campaignKey(campaignIDU64)
		if err := p.redis.Del(ctx, campKey).Err(); err != nil {
			return err
		}
		campaignRows, err := p.db.QueryContext(ctx, `
			SELECT go.creator_id, SUM(go.charged_coins)
			FROM gift_orders go
			JOIN live_sessions ls ON go.live_session_id = ls.live_session_id
			WHERE go.status = 'ACCEPTED' AND ls.campaign_id = ?
			GROUP BY go.creator_id
		`, campaignID.Int64)
		if err != nil {
			return err
		}
		defer func() {
			_ = campaignRows.Close()
		}()
		campaignZ := make([]redis.Z, 0)
		for campaignRows.Next() {
			var creatorID uint64
			var points int64
			if err := campaignRows.Scan(&creatorID, &points); err != nil {
				return err
			}
			campaignZ = append(campaignZ, redis.Z{Member: strconv.FormatUint(creatorID, 10), Score: float64(points)})
		}
		if err := campaignRows.Err(); err != nil {
			return err
		}
		if len(campaignZ) > 0 {
			if err := p.redis.ZAdd(ctx, campKey, campaignZ...).Err(); err != nil {
				return err
			}
		}
	}

	return nil
}

func int64ToUint64(v int64, name string) (uint64, error) {
	if v < 0 {
		return 0, fmt.Errorf("%s overflow uint64: %d", name, v)
	}
	return uint64(v), nil
}

func contributorKey(liveSessionID uint64) string {
	return fmt.Sprintf("lb:contributors:%d", liveSessionID)
}

func campaignKey(campaignID uint64) string {
	return fmt.Sprintf("lb:campaign:%d", campaignID)
}

func matchScoreKey(matchID uint64) string {
	return fmt.Sprintf("match:score:%d", matchID)
}

func isDup(err error) bool {
	myErr, ok := err.(*mysqlerr.MySQLError)
	if !ok {
		return false
	}
	return myErr.Number == 1062
}
