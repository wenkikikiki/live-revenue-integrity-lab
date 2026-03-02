# ADR 0003: Request Idempotency by (actor, request_id) + Body Hash

## Status
Accepted

## Decision
For recharge and gift APIs, deduplicate by actor/request key and reject reused keys with different payloads.
Persist body hash and replay the original response for exact duplicates.

## Consequences
- Duplicate retries are safe under network retry storms.
- Same key with different payloads is explicitly blocked (`IDEMPOTENCY_PAYLOAD_MISMATCH`).
