# ADR 0002: Transactional Outbox for Kafka Publication

## Status
Accepted

## Decision
All asynchronous side effects must originate from rows in `outbox_events` written inside the same DB transaction as business state.
Outbox relay publishes and then marks rows as published.

## Consequences
- No committed business write can lose its corresponding event.
- Relay restarts are safe: unpublished rows remain available.
- Kafka publish is at-least-once; consumers must be idempotent.
