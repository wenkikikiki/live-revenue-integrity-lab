#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
API_PORT="${API_PORT:-8080}"
API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:${API_PORT}}"

compose() {
  (
    cd "$ROOT_DIR"
    docker compose "$@"
  )
}

step() {
  printf '\n== %s ==\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

pretty_json() {
  local raw="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -m json.tool <<<"$raw"
    return
  fi
  printf '%s\n' "$raw"
}

start_stack() {
  local mysql_id redis_id kafka_id api_id
  mysql_id="$(compose ps -q mysql)"
  redis_id="$(compose ps -q redis)"
  kafka_id="$(compose ps -q kafka)"
  api_id="$(compose ps -q api)"

  if [[ -n "$mysql_id" && -n "$redis_id" && -n "$kafka_id" && -n "$api_id" ]] &&
    curl -fsS "${API_BASE_URL}/healthz" >/dev/null 2>&1; then
    return
  fi

  step "Starting the Docker stack"
  bash "$ROOT_DIR/scripts/up_stack.sh" >/dev/null
}

wait_for_api() {
  local deadline=$((SECONDS + 120))
  until curl -fsS "${API_BASE_URL}/healthz" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      fail "API did not become healthy at ${API_BASE_URL}/healthz"
    fi
    sleep 2
  done
}

wait_for_service_health() {
  local service="$1"
  local timeout="${2:-120}"
  local deadline=$((SECONDS + timeout))

  while true; do
    local container_id
    container_id="$(compose ps -q "$service")"
    if [[ -n "$container_id" ]]; then
      local status
      status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id" 2>/dev/null || true)"
      if [[ "$status" == "healthy" || "$status" == "running" ]]; then
        return
      fi
      if [[ "$status" == "exited" ]]; then
        fail "service ${service} exited before becoming healthy"
      fi
    fi
    if ((SECONDS >= deadline)); then
      fail "service ${service} did not become healthy"
    fi
    sleep 2
  done
}

wait_for_service_exit_zero() {
  local service="$1"
  local timeout="${2:-120}"
  local deadline=$((SECONDS + timeout))

  while true; do
    local container_id
    container_id="$(compose ps --all -q "$service")"
    if [[ -n "$container_id" ]]; then
      local state
      local exit_code
      state="$(docker inspect --format '{{.State.Status}}' "$container_id" 2>/dev/null || true)"
      exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container_id" 2>/dev/null || true)"
      if [[ "$state" == "exited" && "$exit_code" == "0" ]]; then
        return
      fi
      if [[ "$state" == "exited" && "$exit_code" != "0" ]]; then
        fail "service ${service} exited with code ${exit_code}"
      fi
    fi
    if ((SECONDS >= deadline)); then
      fail "service ${service} did not finish successfully"
    fi
    sleep 2
  done
}

post_json() {
  local path="$1"
  local body="$2"
  curl -fsS -X POST "${API_BASE_URL}${path}" \
    -H 'Content-Type: application/json' \
    -d "$body"
}

post_empty() {
  local path="$1"
  curl -fsS -X POST "${API_BASE_URL}${path}"
}

get_json() {
  local path="$1"
  curl -fsS "${API_BASE_URL}${path}"
}

request_status() {
  local method="$1"
  local path="$2"
  local body="$3"
  local out_file="$4"

  if [[ -n "$body" ]]; then
    curl -sS -o "$out_file" -w '%{http_code}' -X "$method" "${API_BASE_URL}${path}" \
      -H 'Content-Type: application/json' \
      -d "$body"
    return
  fi

  curl -sS -o "$out_file" -w '%{http_code}' -X "$method" "${API_BASE_URL}${path}"
}

mysql_value() {
  local query="$1"
  compose exec -T mysql mysql -N -B -uroot -proot live_revenue -e "$query" | tr -d '\r'
}

mysql_exec() {
  local query="$1"
  compose exec -T mysql mysql -uroot -proot live_revenue -e "$query"
}

redis_value() {
  compose exec -T redis redis-cli --raw "$@" | tr -d '\r'
}

assert_eq() {
  local actual="$1"
  local expected="$2"
  local message="$3"
  if [[ "$actual" != "$expected" ]]; then
    fail "${message}: expected '${expected}', got '${actual}'"
  fi
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    fail "${message}: expected to find '${needle}'"
  fi
}

wait_for_mysql_value() {
  local query="$1"
  local expected="$2"
  local message="$3"
  local timeout="${4:-60}"
  local deadline=$((SECONDS + timeout))

  while true; do
    local actual
    actual="$(mysql_value "$query")"
    if [[ "$actual" == "$expected" ]]; then
      return
    fi
    if ((SECONDS >= deadline)); then
      fail "${message}: expected '${expected}', got '${actual}'"
    fi
    sleep 2
  done
}

wait_for_redis_score() {
  local key="$1"
  local member="$2"
  local expected="$3"
  local message="$4"
  local timeout="${5:-60}"
  local deadline=$((SECONDS + timeout))

  while true; do
    local actual
    actual="$(redis_value ZSCORE "$key" "$member")"
    if [[ "$actual" == "$expected" ]]; then
      return
    fi
    if ((SECONDS >= deadline)); then
      fail "${message}: expected '${expected}', got '${actual}'"
    fi
    sleep 2
  done
}

wait_for_redis_hash_value() {
  local key="$1"
  local field="$2"
  local expected="$3"
  local message="$4"
  local timeout="${5:-60}"
  local deadline=$((SECONDS + timeout))

  while true; do
    local actual
    actual="$(redis_value HGET "$key" "$field")"
    if [[ "$actual" == "$expected" ]]; then
      return
    fi
    if ((SECONDS >= deadline)); then
      fail "${message}: expected '${expected}', got '${actual}'"
    fi
    sleep 2
  done
}
