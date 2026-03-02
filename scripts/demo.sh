#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$(go env GOPATH)/bin:$PATH"

cleanup() {
  if [[ -n "${API_PID:-}" ]]; then
    kill "$API_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

cd "$ROOT_DIR"

make up
make goose-up

docker exec lr_mysql mysql -uroot -proot -e "USE live_revenue; UPDATE live_sessions SET status='OPEN', closed_at=NULL WHERE live_session_id=9001; UPDATE live_matches SET status='OPEN' WHERE match_id=8001;" >/dev/null

go run ./cmd/api >/tmp/live_revenue_api.log 2>&1 &
API_PID=$!
sleep 2

RUN_ID="$(date +%s)"

curl -sS -X POST http://localhost:8080/v1/wallets/recharges \
  -H 'Content-Type: application/json' \
  -d "{\"request_id\":\"demo-recharge-${RUN_ID}\",\"viewer_id\":2001,\"coins\":100,\"payment_ref\":\"pay-demo-${RUN_ID}\"}"

echo

curl -sS -X POST http://localhost:8080/v1/gifts \
  -H 'Content-Type: application/json' \
  -d "{\"request_id\":\"demo-gift-${RUN_ID}\",\"viewer_id\":2001,\"creator_id\":1001,\"live_session_id\":9001,\"match_id\":8001,\"gift_id\":\"ROSE\",\"quantity\":1,\"sent_at_ms\":1735689600000}"

echo

curl -sS -X POST http://localhost:8080/v1/lives/9001/close

echo

go run ./cmd/settlement-worker --live-session-id=9001

curl -sS http://localhost:8080/v1/settlements/9001

echo

echo "demo complete"
