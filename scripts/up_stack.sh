#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT_DIR/scripts/demo_lib.sh"

compose build app-build

compose up -d mysql redis kafka
wait_for_service_health mysql 120
wait_for_service_health redis 60
wait_for_service_health kafka 120

compose up -d migrate kafka-init
wait_for_service_exit_zero migrate 120
wait_for_service_exit_zero kafka-init 120

compose up -d api outbox-relay leaderboard-projector points-projector settlement-worker
wait_for_service_health api 120
wait_for_api
