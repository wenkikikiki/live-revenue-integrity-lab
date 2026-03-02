# live-revenue-integrity-lab

Correctness-first Go monorepo for LIVE gifting revenue integrity during PK matches.

## What this repo implements

- Exactly-once recharge and gift debit with idempotency keys and body-hash mismatch protection
- Double-entry wallet ledger with zero-sum posting checks
- MySQL outbox + Kafka relay for replayable side effects
- Redis contributor leaderboard + match score projection with deterministic rebuild
- Fan/campaign point ledgers with comment/watch caps and replay-safe dedupe
- Live close -> settlement + reconciliation with PASS/FAIL status
- Prometheus metrics (`/metrics` on API + projector metrics ports)
- k6 smoke/benchmark scripts

## Quick start

### 1. Install tooling

```bash
make tools
```

### 2. Start infra + migrate DB

```bash
make up
make goose-up
```

### 3. Run binaries (separate terminals)

```bash
go run ./cmd/api
go run ./cmd/outbox-relay
go run ./cmd/leaderboard-projector
go run ./cmd/points-projector
go run ./cmd/settlement-worker
```

### 4. One-command demo

```bash
make demo
```

## Verification commands

```bash
make compose-validate
make sqlc-generate && git diff --exit-code
make lint
go test ./...
```

Race detector requires `gcc` + `CGO_ENABLED=1`:

```bash
CGO_ENABLED=1 go test -race ./...
```

## Load scripts

```bash
make k6-smoke
make k6-benchmark
```

## API endpoints

- `POST /v1/wallets/recharges`
- `POST /v1/gifts`
- `POST /v1/internal/comments`
- `POST /v1/internal/watch-minutes`
- `GET /v1/lives/{live_session_id}/contributors?limit=20`
- `GET /v1/campaigns/{campaign_id}/leaderboard?limit=20`
- `POST /v1/lives/{live_session_id}/close`
- `GET /v1/settlements/{live_session_id}`
- `GET /metrics`

## Docs

- [Architecture](docs/architecture.md)
- [Schema invariants](docs/schema.md)
- [ADR log](docs/adr)
- [Benchmark report](docs/benchmark-report.md)
- [Replay/duplicate postmortem](docs/postmortem-replay-duplicate.md)
- [API walkthrough](docs/api-walkthrough.md)
