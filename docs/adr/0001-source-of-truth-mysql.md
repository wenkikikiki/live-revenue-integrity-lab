# ADR 0001: MySQL as Source of Truth, Redis as Projection

## Status
Accepted

## Decision
Use MySQL transactional tables as the only durable source of truth for wallet and gift state.
Use Redis only for low-latency read projections.

## Consequences
- Rebuild commands can restore Redis from MySQL if Redis is wiped.
- Replay safety is guaranteed by `consumer_dedupe` in MySQL.
- Eventual consistency is accepted for projection lag.
