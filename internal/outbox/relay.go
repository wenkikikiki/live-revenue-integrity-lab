// Package outbox publishes durable outbox rows to Kafka.
package outbox

import (
	"context"
	"database/sql"
	"time"

	"github.com/IBM/sarama"
)

// Producer is the subset of sarama SyncProducer used by relay.
type Producer interface {
	SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error)
	Close() error
}

// Relay republishes committed outbox rows to Kafka and marks them published.
type Relay struct {
	db       *sql.DB
	producer Producer
	batch    int
}

// New creates a relay instance.
func New(db *sql.DB, producer Producer, batchSize int) *Relay {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Relay{db: db, producer: producer, batch: batchSize}
}

type row struct {
	EventID  uint64
	Topic    string
	EventKey string
	Payload  string
}

// RunOnce publishes one batch and marks rows published in one transaction.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, topic, event_key, CAST(payload_json AS CHAR)
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, r.batch)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = rows.Close()
	}()

	batch := make([]row, 0, r.batch)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.EventID, &r.Topic, &r.EventKey, &r.Payload); err != nil {
			return 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(batch) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}

	now := time.Now().UTC()
	for _, e := range batch {
		_, _, err := r.producer.SendMessage(&sarama.ProducerMessage{
			Topic: e.Topic,
			Key:   sarama.StringEncoder(e.EventKey),
			Value: sarama.StringEncoder(e.Payload),
			Headers: []sarama.RecordHeader{
				{Key: []byte("event_id"), Value: []byte(formatUint(e.EventID))},
			},
		})
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE outbox_events
			SET published_at = ?
			WHERE event_id = ?
		`, now, e.EventID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(batch), nil
}

// Close closes the underlying producer.
func (r *Relay) Close() error {
	return r.producer.Close()
}

func formatUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
