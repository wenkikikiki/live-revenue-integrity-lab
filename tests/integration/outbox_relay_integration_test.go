package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/mini_station/live-revenue-integrity-lab/internal/outbox"
)

func TestOutboxRelayPublishesAndMarks(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	broker := kafkaBrokerFromEnv()
	kafkaCfg := sarama.NewConfig()
	kafkaCfg.Version = sarama.V4_1_1_0
	kafkaCfg.Producer.Return.Successes = true
	kafkaCfg.Consumer.Return.Errors = true

	admin, err := sarama.NewClusterAdmin([]string{broker}, kafkaCfg)
	if err != nil {
		t.Skipf("kafka not available at %s: %v", broker, err)
	}
	defer func() {
		_ = admin.Close()
	}()

	topic := fmt.Sprintf("outbox_test_publish_%d", time.Now().UnixNano())
	if err := admin.CreateTopic(topic, &sarama.TopicDetail{NumPartitions: 1, ReplicationFactor: 1}, false); err != nil {
		t.Fatalf("create topic: %v", err)
	}

	insertOutboxRows(ctx, t, db, topic, 5)

	producer, err := sarama.NewSyncProducer([]string{broker}, kafkaCfg)
	if err != nil {
		t.Fatalf("new producer: %v", err)
	}
	relay := outbox.New(db, producer, 100)
	defer func() {
		_ = relay.Close()
	}()

	published, err := relay.RunOnce(ctx)
	if err != nil {
		t.Fatalf("relay run once: %v", err)
	}
	if published != 5 {
		t.Fatalf("expected 5 published rows, got %d", published)
	}

	consumed := consumeFromTopic(t, broker, topic, 5, 15*time.Second)
	if consumed != 5 {
		t.Fatalf("expected 5 consumed messages, got %d", consumed)
	}

	assertPublishedCount(ctx, t, db, topic, 5)
}

func TestOutboxRelayRestartNoLostEvent(t *testing.T) {
	ctx := context.Background()
	db, terminate := startMySQLWithMigrations(ctx, t)
	defer terminate(context.Background())

	broker := kafkaBrokerFromEnv()
	kafkaCfg := sarama.NewConfig()
	kafkaCfg.Version = sarama.V4_1_1_0
	kafkaCfg.Producer.Return.Successes = true
	kafkaCfg.Consumer.Return.Errors = true

	admin, err := sarama.NewClusterAdmin([]string{broker}, kafkaCfg)
	if err != nil {
		t.Skipf("kafka not available at %s: %v", broker, err)
	}
	defer func() {
		_ = admin.Close()
	}()

	topic := fmt.Sprintf("outbox_test_restart_%d", time.Now().UnixNano())
	if err := admin.CreateTopic(topic, &sarama.TopicDetail{NumPartitions: 1, ReplicationFactor: 1}, false); err != nil {
		t.Fatalf("create topic: %v", err)
	}

	insertOutboxRows(ctx, t, db, topic, 3)

	producer1, err := sarama.NewSyncProducer([]string{broker}, kafkaCfg)
	if err != nil {
		t.Fatalf("new producer1: %v", err)
	}
	relay1 := outbox.New(db, producer1, 1)
	published, err := relay1.RunOnce(ctx)
	if err != nil {
		t.Fatalf("relay1 run once: %v", err)
	}
	if published != 1 {
		t.Fatalf("expected relay1 to publish 1 row, got %d", published)
	}
	_ = relay1.Close()

	producer2, err := sarama.NewSyncProducer([]string{broker}, kafkaCfg)
	if err != nil {
		t.Fatalf("new producer2: %v", err)
	}
	relay2 := outbox.New(db, producer2, 10)
	defer func() {
		_ = relay2.Close()
	}()
	published2, err := relay2.RunOnce(ctx)
	if err != nil {
		t.Fatalf("relay2 run once: %v", err)
	}
	if published2 != 2 {
		t.Fatalf("expected relay2 to publish remaining 2 rows, got %d", published2)
	}

	consumed := consumeFromTopic(t, broker, topic, 3, 15*time.Second)
	if consumed != 3 {
		t.Fatalf("expected 3 consumed messages, got %d", consumed)
	}

	assertPublishedCount(ctx, t, db, topic, 3)
}

func kafkaBrokerFromEnv() string {
	if v := os.Getenv("KAFKA_BROKER"); v != "" {
		return v
	}
	return "localhost:9092"
}

func insertOutboxRows(ctx context.Context, t *testing.T, db *sql.DB, topic string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		payload := fmt.Sprintf(`{"event":%d}`, i)
		if _, err := db.ExecContext(ctx, `
			INSERT INTO outbox_events (topic, event_key, payload_json, created_at)
			VALUES (?, ?, ?, UTC_TIMESTAMP(6))
		`, topic, fmt.Sprintf("key-%d", i), payload); err != nil {
			t.Fatalf("insert outbox row %d: %v", i, err)
		}
	}
}

func assertPublishedCount(ctx context.Context, t *testing.T, db *sql.DB, topic string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM outbox_events
		WHERE topic = ? AND published_at IS NOT NULL
	`, topic).Scan(&got); err != nil {
		t.Fatalf("query published count: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d published rows, got %d", want, got)
	}
}

func consumeFromTopic(t *testing.T, broker, topic string, want int, timeout time.Duration) int {
	t.Helper()

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V4_1_1_0
	consumer, err := sarama.NewConsumer([]string{broker}, cfg)
	if err != nil {
		t.Fatalf("new consumer: %v", err)
	}
	defer func() {
		_ = consumer.Close()
	}()

	pc, err := consumer.ConsumePartition(topic, 0, sarama.OffsetOldest)
	if err != nil {
		t.Fatalf("consume partition: %v", err)
	}
	defer func() {
		_ = pc.Close()
	}()

	deadline := time.After(timeout)
	count := 0
	for count < want {
		select {
		case <-deadline:
			return count
		case <-pc.Messages():
			count++
		case err := <-pc.Errors():
			t.Fatalf("consumer error: %v", err)
		}
	}
	return count
}
