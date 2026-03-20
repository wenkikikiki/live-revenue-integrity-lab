#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/demo_lib.sh"

bash "$ROOT_DIR/scripts/reset_demo_state.sh" >/dev/null

RUN_ID="$(date +%s)"

step "Stop the leaderboard projector to simulate a consumer outage"
compose stop leaderboard-projector >/dev/null

step "Accept gifts while Redis projections are stale"
resp="$(post_json "/v1/gifts" "{\"request_id\":\"recovery-gift-${RUN_ID}-1\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"DONUT\",\"quantity\":2,\"sent_at_ms\":1735689600000}")"
pretty_json "$resp"
resp="$(post_json "/v1/gifts" "{\"request_id\":\"recovery-gift-${RUN_ID}-2\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"COFFEE\",\"quantity\":1,\"sent_at_ms\":1735689601000}")"
pretty_json "$resp"

step "MySQL is correct even while the projector is down"
assert_eq "$(mysql_value "SELECT COALESCE(SUM(charged_coins), 0) FROM gift_orders WHERE live_session_id = 9001 AND status = 'ACCEPTED'")" "110" "accepted gift total in MySQL"
contributors_before="$(get_json "/v1/lives/9001/contributors?limit=20")"
pretty_json "$contributors_before"
assert_contains "$contributors_before" "\"contributors\":[]" "contributors should still be empty while Redis is stale"

step "Restart the leaderboard projector and wait for Kafka catch-up"
compose start leaderboard-projector >/dev/null
wait_for_redis_score "lb:contributors:9001" "2001" "110" "contributor score after restart"
wait_for_redis_score "lb:campaign:7001" "1001" "110" "campaign score after restart"
wait_for_redis_hash_value "match:score:8001" "creator_1001_points" "110" "match score after restart"

contributors_after="$(get_json "/v1/lives/9001/contributors?limit=20")"
pretty_json "$contributors_after"

step "Delete Redis state and rebuild from MySQL"
redis_value DEL lb:contributors:9001 lb:campaign:7001 match:score:8001 >/dev/null
compose run --rm leaderboard-projector /usr/local/bin/leaderboard-projector --rebuild-live-session=9001 >/dev/null
wait_for_redis_score "lb:contributors:9001" "2001" "110" "rebuild contributor score"
wait_for_redis_score "lb:campaign:7001" "1001" "110" "rebuild campaign score"
wait_for_redis_hash_value "match:score:8001" "creator_1001_points" "110" "rebuild match score"

step "Raw projection state after rebuild"
printf 'contributors=%s\n' "$(redis_value ZREVRANGE lb:contributors:9001 0 -1 WITHSCORES | paste -sd ' ' -)"
printf 'campaign=%s\n' "$(redis_value ZREVRANGE lb:campaign:7001 0 -1 WITHSCORES | paste -sd ' ' -)"
printf 'match_score=%s\n' "$(redis_value HGETALL match:score:8001 | paste -sd ' ' -)"

step "Recovery demo complete"
