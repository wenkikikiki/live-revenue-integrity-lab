# Architecture

## Write path

1. API validates request + idempotency body hash.
2. MySQL transaction posts wallet transaction + wallet entries + domain rows.
3. Outbox row is inserted in the same transaction.
4. Outbox relay publishes to Kafka and marks `published_at`.

## Async projection path

- `leaderboard-projector`
  - consumes `live.gift.accepted.v1`
  - writes dedupe key in `consumer_dedupe`
  - updates Redis: contributors + match score
  - supports deterministic rebuild from MySQL gift orders

- `points-projector`
  - consumes gift/comment/watch topics
  - writes dedupe key in `consumer_dedupe`
  - updates `fan_point_ledger` and `campaign_point_ledger`
  - enforces comment/watch per-session caps

- `settlement-worker`
  - consumes `live.session.closed.v1`
  - computes settlement aggregate
  - computes reconciliation totals and PASS/FAIL status

## Source of truth

MySQL is the only source of truth. Redis is projection-only and rebuildable.

## Money correctness

- Integer-only amounts (`INT` / `BIGINT`)
- Exactly-once debit keying: `(viewer_id, request_id)` + unique tx keys
- Double-entry zero-sum guard before commit
- Settlement compares accepted gift spend vs actual wallet debit totals
