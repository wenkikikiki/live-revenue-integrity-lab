FROM golang:1.25.8-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
COPY vendor ./vendor
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

RUN --mount=type=cache,target=/root/.cache/go-build \
    GOFLAGS=-mod=vendor CGO_ENABLED=0 go build -o /out/api ./cmd/api && \
    GOFLAGS=-mod=vendor CGO_ENABLED=0 go build -o /out/outbox-relay ./cmd/outbox-relay && \
    GOFLAGS=-mod=vendor CGO_ENABLED=0 go build -o /out/leaderboard-projector ./cmd/leaderboard-projector && \
    GOFLAGS=-mod=vendor CGO_ENABLED=0 go build -o /out/points-projector ./cmd/points-projector && \
    GOFLAGS=-mod=vendor CGO_ENABLED=0 go build -o /out/settlement-worker ./cmd/settlement-worker

FROM busybox:1.37.0

WORKDIR /app

COPY --from=build /out/ /usr/local/bin/
COPY migrations /app/migrations
