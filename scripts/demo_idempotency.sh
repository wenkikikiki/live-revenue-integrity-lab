#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/demo_lib.sh"

bash "$ROOT_DIR/scripts/reset_demo_state.sh" >/dev/null

RUN_ID="$(date +%s)"
REQUEST_ID="idem-gift-${RUN_ID}"
PAYLOAD="{\"request_id\":\"${REQUEST_ID}\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"DONUT\",\"quantity\":2,\"sent_at_ms\":1735689600000}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

step "Fire 12 concurrent retries with the same request_id"
for i in $(seq 1 12); do
  (
    code="$(request_status POST "/v1/gifts" "$PAYLOAD" "${TMP_DIR}/${i}.json")"
    printf '%s\n' "$code" > "${TMP_DIR}/${i}.code"
  ) &
done
wait

created_count="$( (grep -l '"idempotency":"created"' "$TMP_DIR"/*.json 2>/dev/null || true) | wc -l | tr -d ' ' )"
replayed_count="$( (grep -l '"idempotency":"replayed"' "$TMP_DIR"/*.json 2>/dev/null || true) | wc -l | tr -d ' ' )"
status_201_count="$( (grep -l '^201$' "$TMP_DIR"/*.code 2>/dev/null || true) | wc -l | tr -d ' ' )"

assert_eq "$created_count" "1" "exactly one request should create the gift"
assert_eq "$replayed_count" "11" "all other retries should replay the original result"
assert_eq "$status_201_count" "12" "all identical retries should return success"

step "One real write happened even though 12 requests were sent"
printf 'created_responses=%s\n' "$created_count"
printf 'replayed_responses=%s\n' "$replayed_count"
printf 'wallet_tx_count=%s\n' "$(mysql_value "SELECT COUNT(*) FROM wallet_transactions WHERE actor_user_id = 2001 AND tx_type = 'GIFT_DEBIT' AND request_id = '${REQUEST_ID}'")"
printf 'gift_order_count=%s\n' "$(mysql_value "SELECT COUNT(*) FROM gift_orders WHERE viewer_id = 2001 AND request_id = '${REQUEST_ID}'")"
printf 'viewer_balance=%s\n' "$(mysql_value "SELECT available_balance FROM wallet_accounts WHERE user_id = 2001 AND currency = 'COIN'")"
printf 'request_outbox_events=%s\n' "$(mysql_value "SELECT COUNT(*) FROM outbox_events WHERE JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.request_id')) = '${REQUEST_ID}'")"

assert_eq "$(mysql_value "SELECT COUNT(*) FROM wallet_transactions WHERE actor_user_id = 2001 AND tx_type = 'GIFT_DEBIT' AND request_id = '${REQUEST_ID}'")" "1" "wallet debit count"
assert_eq "$(mysql_value "SELECT COUNT(*) FROM gift_orders WHERE viewer_id = 2001 AND request_id = '${REQUEST_ID}'")" "1" "gift order count"
assert_eq "$(mysql_value "SELECT available_balance FROM wallet_accounts WHERE user_id = 2001 AND currency = 'COIN'")" "940" "viewer balance after one debit"

step "Reuse the same request_id with a different body"
MISMATCH_PAYLOAD="{\"request_id\":\"${REQUEST_ID}\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"DONUT\",\"quantity\":3,\"sent_at_ms\":1735689600000}"
MISMATCH_FILE="${TMP_DIR}/mismatch.json"
MISMATCH_CODE="$(request_status POST "/v1/gifts" "$MISMATCH_PAYLOAD" "$MISMATCH_FILE")"
assert_eq "$MISMATCH_CODE" "409" "payload mismatch should return 409"
assert_contains "$(cat "$MISMATCH_FILE")" "IDEMPOTENCY_PAYLOAD_MISMATCH" "payload mismatch response"
pretty_json "$(cat "$MISMATCH_FILE")"

step "Final raw-data verification"
printf 'gift_order_status=%s\n' "$(mysql_value "SELECT status FROM gift_orders WHERE viewer_id = 2001 AND request_id = '${REQUEST_ID}'")"
printf 'contributors=%s\n' "$(redis_value ZREVRANGE lb:contributors:9001 0 -1 WITHSCORES | paste -sd ' ' -)"

step "Idempotency demo complete"
