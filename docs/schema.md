# Schema and Invariants

This schema is the durable source of truth. Redis is projection-only.

## Invariant mapping

1. Exactly-once debit: `wallet_transactions.uq_wallet_tx` and `gift_orders.uq_gift_req` guarantee one debit/order per `(viewer_id, request_id)`.
2. Balanced ledger: every write creates matching signed entries in `wallet_entries` for a `wallet_transactions.tx_id`.
3. Integer money: all price/balance/points columns are `INT`/`BIGINT` integer columns.
4. Server-derived values: `charged_coins`, `match_points_added`, and `diamond_reward` are persisted in `gift_orders` and must come from server-side rule computation.
5. Eligibility: enforced by API on top of `users` fields (`age_years`, `region_code`, `account_standing`, `can_go_live`, `live_gifts_enabled`, `account_type`).
6. Projection-only leaderboard: leaderboard totals are rebuilt from `gift_orders`, `fan_point_ledger`, and `campaign_point_ledger`.
7. Rebuildability: projection workers recompute Redis keys from MySQL tables.
8. Settlement: `stream_settlements` + `reconciliation_results` compare accepted gifts vs ledger debits.
9. Replay safety: `consumer_dedupe` enforces idempotent event handling per consumer and `event_id`.

## Tables

- `users`
- `live_sessions`
- `live_matches`
- `gift_catalog`
- `wallet_accounts`
- `wallet_transactions`
- `wallet_entries`
- `recharge_requests` (stores recharge idempotency body hash)
- `gift_orders`
- `outbox_events`
- `consumer_dedupe`
- `fan_point_ledger`
- `campaign_point_ledger`
- `stream_settlements`
- `reconciliation_results`
