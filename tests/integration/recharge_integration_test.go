package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRechargeAndIdempotency(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	svc := service.New(db)

	first, appErr, err := svc.Recharge(ctx, service.RechargeRequest{
		RequestID:  "r-1",
		ViewerID:   2001,
		Coins:      123,
		PaymentRef: "pay-1",
	})
	if err != nil {
		t.Fatalf("first recharge err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("first recharge app err: %+v", *appErr)
	}
	if first.NewBalance != 1123 {
		t.Fatalf("want new balance 1123, got %d", first.NewBalance)
	}

	second, appErr, err := svc.Recharge(ctx, service.RechargeRequest{
		RequestID:  "r-1",
		ViewerID:   2001,
		Coins:      123,
		PaymentRef: "pay-1",
	})
	if err != nil {
		t.Fatalf("duplicate recharge err: %v", err)
	}
	if appErr != nil {
		t.Fatalf("duplicate recharge app err: %+v", *appErr)
	}
	if second.TxID != first.TxID {
		t.Fatalf("expected duplicate to return same tx id, got %d vs %d", second.TxID, first.TxID)
	}
	if second.NewBalance != first.NewBalance {
		t.Fatalf("expected same balance replay, got %d vs %d", second.NewBalance, first.NewBalance)
	}

	_, appErr, err = svc.Recharge(ctx, service.RechargeRequest{
		RequestID:  "r-1",
		ViewerID:   2001,
		Coins:      555,
		PaymentRef: "pay-1",
	})
	if err != nil {
		t.Fatalf("mismatch duplicate err: %v", err)
	}
	if appErr == nil || appErr.Code != "IDEMPOTENCY_PAYLOAD_MISMATCH" {
		t.Fatalf("expected IDEMPOTENCY_PAYLOAD_MISMATCH, got %#v", appErr)
	}

	var balance int64
	if err := db.QueryRowContext(ctx, `SELECT available_balance FROM wallet_accounts WHERE user_id = 2001 AND currency='COIN'`).Scan(&balance); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if balance != 1123 {
		t.Fatalf("expected balance 1123 after idempotent duplicate, got %d", balance)
	}

	var sum int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM wallet_entries we
		JOIN wallet_transactions wt ON wt.tx_id = we.tx_id
		WHERE wt.tx_type = 'RECHARGE' AND wt.actor_user_id = 2001 AND wt.request_id = 'r-1'
	`).Scan(&sum); err != nil {
		t.Fatalf("query ledger sum: %v", err)
	}
	if sum != 0 {
		t.Fatalf("expected zero-sum recharge entries, got %d", sum)
	}
}

func startMySQLWithMigrations(ctx context.Context, t *testing.T) (*sql.DB, func(context.Context)) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.4.8",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "root",
			"MYSQL_DATABASE":      "live_revenue",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("ready for connections").WithOccurrence(2),
			wait.ForListeningPort("3306/tcp"),
		).WithDeadline(2 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start mysql container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("mysql host: %v", err)
	}
	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("mysql port: %v", err)
	}

	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/live_revenue?parseTime=true&multiStatements=true", host, port.Port())
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("open mysql: %v", err)
	}

	deadline := time.Now().Add(1 * time.Minute)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			_ = container.Terminate(ctx)
			t.Fatal("mysql ping timeout")
		}
		time.Sleep(500 * time.Millisecond)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	migrationDir := filepath.Join(repoRoot, "migrations")
	if err := goose.SetDialect("mysql"); err != nil {
		_ = db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("set goose dialect: %v", err)
	}
	if err := goose.Up(db, migrationDir); err != nil {
		_ = db.Close()
		_ = container.Terminate(ctx)
		t.Fatalf("goose up: %v", err)
	}

	terminate := func(termCtx context.Context) {
		_ = db.Close()
		_ = container.Terminate(termCtx)
	}
	return db, terminate
}
