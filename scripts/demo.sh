#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/demo_lib.sh"

bash "$ROOT_DIR/scripts/reset_demo_state.sh" >/dev/null

RUN_ID="$(date +%s)"

step "Recharge viewer 2001"
resp="$(post_json "/v1/wallets/recharges" "{\"request_id\":\"happy-recharge-${RUN_ID}-2001\",\"viewer_id\":2001,\"coins\":250,\"payment_ref\":\"happy-pay-${RUN_ID}-2001\"}")"
pretty_json "$resp"

step "Recharge viewer 2002"
resp="$(post_json "/v1/wallets/recharges" "{\"request_id\":\"happy-recharge-${RUN_ID}-2002\",\"viewer_id\":2002,\"coins\":50,\"payment_ref\":\"happy-pay-${RUN_ID}-2002\"}")"
pretty_json "$resp"

step "Send accepted gifts into live session 9001 / match 8001"
resp="$(post_json "/v1/gifts" "{\"request_id\":\"happy-gift-${RUN_ID}-1\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"DONUT\",\"quantity\":2,\"sent_at_ms\":1735689600000}")"
pretty_json "$resp"
resp="$(post_json "/v1/gifts" "{\"request_id\":\"happy-gift-${RUN_ID}-2\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"COFFEE\",\"quantity\":1,\"sent_at_ms\":1735689601000}")"
pretty_json "$resp"
resp="$(post_json "/v1/gifts" "{\"request_id\":\"happy-gift-${RUN_ID}-3\",\"viewer_id\":2002,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"HEART\",\"quantity\":3,\"sent_at_ms\":1735689602000}")"
pretty_json "$resp"

step "Emit synthetic fan-point events"
resp="$(post_json "/v1/internal/comments" "{\"event_id\":\"happy-comment-${RUN_ID}\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001}")"
pretty_json "$resp"
resp="$(post_json "/v1/internal/comments" "{\"event_id\":\"happy-comment-${RUN_ID}-2\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001}")"
pretty_json "$resp"
resp="$(post_json "/v1/internal/comments" "{\"event_id\":\"happy-comment-${RUN_ID}-3\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001}")"
pretty_json "$resp"
resp="$(post_json "/v1/internal/watch-minutes" "{\"event_id\":\"happy-watch-${RUN_ID}\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"minutes\":5}")"
pretty_json "$resp"

step "Wait for asynchronous projections to catch up"
wait_for_redis_score "lb:contributors:9001" "2001" "110" "viewer 2001 contributor score"
wait_for_redis_score "lb:contributors:9001" "2002" "15" "viewer 2002 contributor score"
wait_for_redis_score "lb:campaign:7001" "1001" "125" "campaign leaderboard score"
wait_for_redis_hash_value "match:score:8001" "creator_1001_points" "125" "match score"
wait_for_mysql_value "SELECT COALESCE(SUM(points), 0) FROM fan_point_ledger WHERE viewer_id = 2001 AND live_session_id = 9001" "118" "fan points for viewer 2001"
wait_for_mysql_value "SELECT COALESCE(SUM(points), 0) FROM fan_point_ledger WHERE viewer_id = 2002 AND live_session_id = 9001" "15" "fan points for viewer 2002"

step "Read the user-facing projections"
resp="$(get_json "/v1/lives/9001/contributors?limit=20")"
pretty_json "$resp"
resp="$(get_json "/v1/campaigns/7001/leaderboard?limit=20")"
pretty_json "$resp"

step "Close the live session and wait for settlement"
resp="$(post_empty "/v1/lives/9001/close")"
pretty_json "$resp"
wait_for_mysql_value "SELECT status FROM reconciliation_results WHERE live_session_id = 9001" "PASS" "reconciliation status"
resp="$(get_json "/v1/settlements/9001")"
pretty_json "$resp"

step "Raw data checks"
printf 'accepted_gift_coin_total=%s\n' "$(mysql_value "SELECT COALESCE(SUM(charged_coins), 0) FROM gift_orders WHERE live_session_id = 9001 AND status = 'ACCEPTED'")"
printf 'wallet_gift_debit_total=%s\n' "$(mysql_value "SELECT wallet_gift_debit_total FROM reconciliation_results WHERE live_session_id = 9001")"
printf 'redis_contributors=%s\n' "$(redis_value ZREVRANGE lb:contributors:9001 0 -1 WITHSCORES | paste -sd ' ' -)"
printf 'redis_campaign=%s\n' "$(redis_value ZREVRANGE lb:campaign:7001 0 -1 WITHSCORES | paste -sd ' ' -)"
printf 'redis_match_score=%s\n' "$(redis_value HGETALL match:score:8001 | paste -sd ' ' -)"

step "Happy-path demo complete"
