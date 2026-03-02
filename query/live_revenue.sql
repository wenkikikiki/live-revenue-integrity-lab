-- name: GetWalletAccountForUpdate :one
SELECT user_id, currency, available_balance
FROM wallet_accounts
WHERE user_id = ? AND currency = 'COIN'
FOR UPDATE;

-- name: CreateWalletAccount :exec
INSERT INTO wallet_accounts (user_id, currency, available_balance)
VALUES (?, 'COIN', 0);

-- name: GetRechargeRequest :one
SELECT viewer_id, request_id, body_hash, coins, payment_ref, resulting_balance, tx_id, created_at
FROM recharge_requests
WHERE viewer_id = ? AND request_id = ?;

-- name: InsertWalletTransaction :execresult
INSERT INTO wallet_transactions (tx_type, actor_user_id, request_id, body_hash, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: InsertWalletEntry :exec
INSERT INTO wallet_entries (tx_id, account_code, amount)
VALUES (?, ?, ?);

-- name: UpdateWalletBalance :exec
UPDATE wallet_accounts
SET available_balance = ?
WHERE user_id = ? AND currency = 'COIN';

-- name: InsertRechargeRequest :exec
INSERT INTO recharge_requests (viewer_id, request_id, body_hash, coins, payment_ref, resulting_balance, tx_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetGiftOrderByRequest :one
SELECT gift_order_id, viewer_id, creator_id, live_session_id, match_id, gift_id, quantity,
       charged_coins, match_points_added, diamond_reward, post_balance, status, reject_code, request_id,
       body_hash, created_at
FROM gift_orders
WHERE viewer_id = ? AND request_id = ?;

-- name: InsertGiftOrder :execresult
INSERT INTO gift_orders (
    viewer_id, creator_id, live_session_id, match_id, gift_id, quantity,
    charged_coins, match_points_added, diamond_reward, post_balance, status, reject_code,
    request_id, body_hash, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertOutboxEvent :execresult
INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
VALUES (?, ?, ?, ?);

-- name: SelectOutboxBatchForPublish :many
SELECT event_id, topic, event_key, payload_json, created_at
FROM outbox_events
WHERE published_at IS NULL
ORDER BY created_at ASC
LIMIT ?
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxPublished :exec
UPDATE outbox_events
SET published_at = ?
WHERE event_id = ?;

-- name: MarkLiveSessionClosed :exec
UPDATE live_sessions
SET status = 'CLOSED', closed_at = ?
WHERE live_session_id = ? AND status = 'OPEN';

-- name: GetLiveSession :one
SELECT live_session_id, creator_id, campaign_id, status, started_at, closed_at
FROM live_sessions
WHERE live_session_id = ?;

-- name: GetLiveMatch :one
SELECT match_id, live_session_id, mode, specific_gift_id, status
FROM live_matches
WHERE match_id = ?;

-- name: GetGiftCatalogItem :one
SELECT gift_id, display_name, coin_price, match_points, diamond_reward, enabled
FROM gift_catalog
WHERE gift_id = ?;

-- name: GetUser :one
SELECT user_id, age_years, region_code, account_standing, can_go_live, live_gifts_enabled, account_type
FROM users
WHERE user_id = ?;

-- name: UpsertSettlement :exec
INSERT INTO stream_settlements (live_session_id, gross_coin_spend, accepted_gift_count, diamond_reward_total, generated_at)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    gross_coin_spend = VALUES(gross_coin_spend),
    accepted_gift_count = VALUES(accepted_gift_count),
    diamond_reward_total = VALUES(diamond_reward_total),
    generated_at = VALUES(generated_at);

-- name: UpsertReconciliation :exec
INSERT INTO reconciliation_results (
    live_session_id, status, wallet_gift_debit_total, gift_order_coin_total, mismatch_count, details_json, generated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    status = VALUES(status),
    wallet_gift_debit_total = VALUES(wallet_gift_debit_total),
    gift_order_coin_total = VALUES(gift_order_coin_total),
    mismatch_count = VALUES(mismatch_count),
    details_json = VALUES(details_json),
    generated_at = VALUES(generated_at);
