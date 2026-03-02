// Package main provides the outbox relay binary.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/mini_station/live-revenue-integrity-lab/internal/config"
	"github.com/mini_station/live-revenue-integrity-lab/internal/db"
	"github.com/mini_station/live-revenue-integrity-lab/internal/outbox"
)

func main() {
	var (
		once     = flag.Bool("once", false, "publish one batch and exit")
		batch    = flag.Int("batch-size", 200, "maximum rows per batch")
		interval = flag.Duration("interval", 500*time.Millisecond, "poll interval")
	)
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	database, err := db.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer func() {
		_ = database.Close()
	}()

	saramaCfg := sarama.NewConfig()
	saramaCfg.Version = sarama.V4_1_1_0
	saramaCfg.Producer.Return.Successes = true
	saramaCfg.Producer.Retry.Max = 3
	saramaCfg.Producer.RequiredAcks = sarama.WaitForAll

	producer, err := sarama.NewSyncProducer(cfg.KafkaBrokers, saramaCfg)
	if err != nil {
		log.Fatalf("open kafka producer: %v", err)
	}

	relay := outbox.New(database, producer, *batch)
	defer func() {
		_ = relay.Close()
	}()

	if *once {
		n, err := relay.RunOnce(ctx)
		if err != nil {
			log.Fatalf("relay run once: %v", err)
		}
		log.Printf("relay published %d events", n)
		return
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-sigs:
			return
		case <-ticker.C:
			n, err := relay.RunOnce(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Printf("relay publish error: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("relay published %d events", n)
			}
		}
	}
}
