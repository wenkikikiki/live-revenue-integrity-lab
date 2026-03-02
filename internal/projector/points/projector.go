// Package points projects fan and campaign points from LIVE events.
package points

import (
	"context"
	"database/sql"
	"errors"
	"time"

	mysqlerr "github.com/go-sql-driver/mysql"
	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
)

const consumerName = "points-projector"

// Projector updates point ledgers from incoming events.
type Projector struct {
	db *sql.DB
}

// New creates a points projector.
func New(db *sql.DB) *Projector {
	return &Projector{db: db}
}

// ApplyGift records fan and campaign points for accepted gifts.
func (p *Projector) ApplyGift(ctx context.Context, eventID uint64, evt events.LiveGiftAcceptedV1) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := isDuplicateOrErr(ctx, tx, eventID); err != nil {
		if errors.Is(err, errDuplicateEvent) {
			if err := tx.Commit(); err != nil {
				return err
			}
			return nil
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fan_point_ledger (event_id, viewer_id, creator_id, live_session_id, points, reason)
		VALUES (?, ?, ?, ?, ?, 'GIFT')
	`, eventID, evt.ViewerID, evt.CreatorID, evt.LiveSessionID, evt.ChargedCoins); err != nil {
		return err
	}

	if evt.CampaignID != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO campaign_point_ledger (event_id, campaign_id, creator_id, points, reason)
			VALUES (?, ?, ?, ?, 'GIFT')
		`, eventID, *evt.CampaignID, evt.CreatorID, evt.ChargedCoins); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ApplyComment records fan points for comments with a per-session cap.
func (p *Projector) ApplyComment(ctx context.Context, eventID uint64, evt events.LiveCommentCreatedV1) error {
	return p.applyCapped(ctx, eventID, evt.ViewerID, evt.CreatorID, evt.LiveSessionID, "COMMENT", 20)
}

// ApplyWatchMinute records fan points for watch-minute events with a cap.
func (p *Projector) ApplyWatchMinute(ctx context.Context, eventID uint64, evt events.LiveWatchMinuteV1) error {
	return p.applyCapped(ctx, eventID, evt.ViewerID, evt.CreatorID, evt.LiveSessionID, "WATCH_MINUTE", 60)
}

func (p *Projector) applyCapped(ctx context.Context, eventID, viewerID, creatorID, liveSessionID uint64, reason string, capCount int) error {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := isDuplicateOrErr(ctx, tx, eventID); err != nil {
		if errors.Is(err, errDuplicateEvent) {
			if err := tx.Commit(); err != nil {
				return err
			}
			return nil
		}
		return err
	}

	var currentCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM fan_point_ledger
		WHERE viewer_id = ? AND live_session_id = ? AND reason = ?
	`, viewerID, liveSessionID, reason).Scan(&currentCount); err != nil {
		return err
	}
	if currentCount >= capCount {
		return tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fan_point_ledger (event_id, viewer_id, creator_id, live_session_id, points, reason)
		VALUES (?, ?, ?, ?, 1, ?)
	`, eventID, viewerID, creatorID, liveSessionID, reason); err != nil {
		return err
	}
	return tx.Commit()
}

var errDuplicateEvent = errors.New("duplicate event")

func isDuplicateOrErr(ctx context.Context, tx *sql.Tx, eventID uint64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO consumer_dedupe (consumer_name, event_id, applied_at)
		VALUES (?, ?, ?)
	`, consumerName, eventID, time.Now().UTC()); err != nil {
		if isDup(err) {
			return errDuplicateEvent
		}
		return err
	}
	return nil
}

func isDup(err error) bool {
	myErr, ok := err.(*mysqlerr.MySQLError)
	if !ok {
		return false
	}
	return myErr.Number == 1062
}
