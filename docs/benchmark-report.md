# Benchmark Report

## Scope
k6 scripts are provided for:
- smoke: `scripts/k6/smoke.js`
- benchmark: `scripts/k6/benchmark.js`

## Target SLOs
- `POST /v1/gifts`: p95 <= 75ms, p99 <= 150ms
- projection lag p95 <= 2s
- successful write-path error rate < 0.1%

## Current repo state
- Functional load scripts are included.
- API latency/reject metrics are exported via Prometheus.
- Projector lag metrics are exported via Prometheus.

## Repro command
```bash
make k6-benchmark
```

## Notes
Run benchmark only against an isolated environment with workers enabled (`api`, `outbox-relay`, `leaderboard-projector`, `points-projector`).
