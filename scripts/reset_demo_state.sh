#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/demo_lib.sh"

start_stack

step "Resetting mutable MySQL tables and Redis projections"
compose stop outbox-relay leaderboard-projector points-projector settlement-worker >/dev/null 2>&1 || true

mysql_exec "
DELETE FROM reconciliation_results;
DELETE FROM stream_settlements;
DELETE FROM campaign_point_ledger;
DELETE FROM fan_point_ledger;
DELETE FROM consumer_dedupe;
DELETE FROM outbox_events;
DELETE FROM gift_orders;
DELETE FROM recharge_requests;
DELETE FROM wallet_entries;
DELETE FROM wallet_transactions;

UPDATE wallet_accounts
SET available_balance = CASE user_id
  WHEN 1001 THEN 50000
  WHEN 1002 THEN 50000
  WHEN 2001 THEN 1000
  WHEN 2002 THEN 1000
  ELSE available_balance
END
WHERE currency = 'COIN' AND user_id IN (1001, 1002, 2001, 2002);

UPDATE live_sessions
SET status = 'OPEN', closed_at = NULL
WHERE live_session_id IN (9001, 9002);

UPDATE live_matches
SET status = 'OPEN', mode = 'ALL_GIFTS', specific_gift_id = NULL
WHERE match_id = 8001;

UPDATE live_matches
SET status = 'OPEN', mode = 'SPECIFIC_GIFT', specific_gift_id = 'ROSE'
WHERE match_id = 8002;
"

redis_value FLUSHALL >/dev/null

compose start outbox-relay leaderboard-projector points-projector settlement-worker >/dev/null
wait_for_api

step "Demo state is clean"
printf 'API: %s\n' "$API_BASE_URL"
