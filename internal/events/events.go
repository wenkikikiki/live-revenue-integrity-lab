// Package events defines outbox/Kafka event payloads.
package events

import "encoding/json"

// WalletTransactionPostedV1 is emitted whenever a wallet transaction commits.
type WalletTransactionPostedV1 struct {
	TxID         uint64 `json:"tx_id"`
	TxType       string `json:"tx_type"`
	ActorUserID  uint64 `json:"actor_user_id"`
	RequestID    string `json:"request_id"`
	DeltaCoins   int64  `json:"delta_coins"`
	BalanceAfter int64  `json:"balance_after"`
}

// LiveGiftAcceptedV1 is emitted for accepted gifts.
type LiveGiftAcceptedV1 struct {
	EventType     string  `json:"event_type"`
	GiftOrderID   uint64  `json:"gift_order_id"`
	ViewerID      uint64  `json:"viewer_id"`
	CreatorID     uint64  `json:"creator_id"`
	LiveSessionID uint64  `json:"live_session_id"`
	CampaignID    *uint64 `json:"campaign_id,omitempty"`
	MatchID       *uint64 `json:"match_id,omitempty"`
	GiftID        string  `json:"gift_id"`
	Quantity      uint16  `json:"quantity"`
	ChargedCoins  uint32  `json:"charged_coins"`
	MatchPoints   uint32  `json:"match_points_added"`
	DiamondReward uint32  `json:"diamond_reward"`
	RequestID     string  `json:"request_id"`
}

// LiveGiftRejectedV1 is emitted for rejected gifts.
type LiveGiftRejectedV1 struct {
	EventType     string  `json:"event_type"`
	ViewerID      uint64  `json:"viewer_id"`
	CreatorID     uint64  `json:"creator_id"`
	LiveSessionID uint64  `json:"live_session_id"`
	MatchID       *uint64 `json:"match_id,omitempty"`
	GiftID        string  `json:"gift_id"`
	Quantity      uint16  `json:"quantity"`
	RequestID     string  `json:"request_id"`
	RejectCode    string  `json:"reject_code"`
}

// LiveCommentCreatedV1 is emitted for synthetic comment events.
type LiveCommentCreatedV1 struct {
	EventID       string `json:"event_id"`
	ViewerID      uint64 `json:"viewer_id"`
	CreatorID     uint64 `json:"creator_id"`
	LiveSessionID uint64 `json:"live_session_id"`
}

// LiveWatchMinuteV1 is emitted for synthetic watch-minute events.
type LiveWatchMinuteV1 struct {
	EventID       string `json:"event_id"`
	ViewerID      uint64 `json:"viewer_id"`
	CreatorID     uint64 `json:"creator_id"`
	LiveSessionID uint64 `json:"live_session_id"`
}

// LiveSessionClosedV1 is emitted when a live session closes.
type LiveSessionClosedV1 struct {
	LiveSessionID uint64 `json:"live_session_id"`
	ClosedAtMs    int64  `json:"closed_at_ms"`
}

// MarshalJSONString marshals a payload to a JSON string.
func MarshalJSONString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
