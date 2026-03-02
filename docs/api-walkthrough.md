# API Walkthrough

## Recharge

```bash
curl -sS -X POST http://localhost:8080/v1/wallets/recharges \
  -H 'Content-Type: application/json' \
  -d '{"request_id":"demo-recharge-1","viewer_id":2001,"coins":100,"payment_ref":"pay-demo-1"}'
```

## Send gift

```bash
curl -sS -X POST http://localhost:8080/v1/gifts \
  -H 'Content-Type: application/json' \
  -d '{"request_id":"demo-gift-1","viewer_id":2001,"creator_id":1001,"live_session_id":9001,"match_id":8001,"gift_id":"ROSE","quantity":1,"sent_at_ms":1735689600000}'
```

## Close live

```bash
curl -sS -X POST http://localhost:8080/v1/lives/9001/close
```

## Generate settlement (worker one-shot)

```bash
go run ./cmd/settlement-worker --live-session-id=9001
```

## Read settlement

```bash
curl -sS http://localhost:8080/v1/settlements/9001
```

## Read contributors

```bash
curl -sS 'http://localhost:8080/v1/lives/9001/contributors?limit=20'
```
