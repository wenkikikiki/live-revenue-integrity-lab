package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
)

// InternalCommentRequest is the synthetic comment ingest payload.
type InternalCommentRequest struct {
	EventID       string `json:"event_id"`
	ViewerID      uint64 `json:"viewer_id"`
	CreatorID     uint64 `json:"creator_id"`
	LiveSessionID uint64 `json:"live_session_id"`
}

// InternalWatchMinutesRequest is the synthetic watch-minute ingest payload.
type InternalWatchMinutesRequest struct {
	EventID       string `json:"event_id"`
	ViewerID      uint64 `json:"viewer_id"`
	CreatorID     uint64 `json:"creator_id"`
	LiveSessionID uint64 `json:"live_session_id"`
	Minutes       uint16 `json:"minutes"`
}

// EnqueueCommentEvent writes one comment outbox event.
func (s *Service) EnqueueCommentEvent(ctx context.Context, req InternalCommentRequest) *AppError {
	if strings.TrimSpace(req.EventID) == "" || req.ViewerID == 0 || req.CreatorID == 0 || req.LiveSessionID == 0 {
		return errValidation
	}
	payload, err := events.MarshalJSONString(events.LiveCommentCreatedV1{
		EventID:       req.EventID,
		ViewerID:      req.ViewerID,
		CreatorID:     req.CreatorID,
		LiveSessionID: req.LiveSessionID,
	})
	if err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
		VALUES ('live.comment.created.v1', ?, ?, ?)
	`, fmt.Sprintf("%d", req.LiveSessionID), payload, time.Now().UTC()); err != nil {
		return &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	return nil
}

// EnqueueWatchMinuteEvents writes one outbox event per watch minute.
func (s *Service) EnqueueWatchMinuteEvents(ctx context.Context, req InternalWatchMinutesRequest) (int, *AppError) {
	if strings.TrimSpace(req.EventID) == "" || req.ViewerID == 0 || req.CreatorID == 0 || req.LiveSessionID == 0 || req.Minutes == 0 {
		return 0, errValidation
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for i := uint16(1); i <= req.Minutes; i++ {
		payload, err := events.MarshalJSONString(events.LiveWatchMinuteV1{
			EventID:       fmt.Sprintf("%s-%d", req.EventID, i),
			ViewerID:      req.ViewerID,
			CreatorID:     req.CreatorID,
			LiveSessionID: req.LiveSessionID,
		})
		if err != nil {
			return 0, &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
			VALUES ('live.watch.minute.v1', ?, ?, ?)
		`, fmt.Sprintf("%d", req.LiveSessionID), payload, time.Now().UTC()); err != nil {
			return 0, &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, &AppError{HTTPStatus: 500, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	return int(req.Minutes), nil
}
