# Incident Postmortem: Replay and Duplicate Storm

## Summary
A retry storm sent high-rate duplicate `POST /v1/gifts` requests for identical `(viewer_id, request_id)` keys while projectors restarted.

## Impact
- No double debit occurred.
- Temporary projection lag increased during consumer restart.
- Final settlement and reconciliation remained deterministic.

## Root cause
Client retry policy retried aggressively after transient upstream timeout.

## What worked
- DB idempotency keys prevented duplicate debits.
- Outbox relay preserved committed events during restarts.
- `consumer_dedupe` prevented replay amplification.
- Rebuild command restored Redis projection after restart.

## Action items
1. Add client-side jitter/backoff recommendations to API contract docs.
2. Alert on projection lag histogram threshold breaches.
3. Keep replay tests in CI to prevent regressions.
