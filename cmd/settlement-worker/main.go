// Package main provides the settlement worker binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/mini_station/live-revenue-integrity-lab/internal/config"
	"github.com/mini_station/live-revenue-integrity-lab/internal/db"
	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
)

type handler struct {
	svc *service.Service
}

func (h *handler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *handler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *handler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var evt events.LiveSessionClosedV1
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			session.MarkMessage(msg, "bad_payload")
			continue
		}
		if _, err := h.svc.GenerateSettlement(session.Context(), evt.LiveSessionID); err != nil {
			return err
		}
		session.MarkMessage(msg, "settled")
	}
	return nil
}

func main() {
	liveSessionID := flag.Uint64("live-session-id", 0, "generate settlement once for a live session id and exit")
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

	svc := service.New(database)
	if *liveSessionID != 0 {
		res, err := svc.GenerateSettlement(ctx, *liveSessionID)
		if err != nil {
			log.Fatalf("generate settlement: %v", err)
		}
		log.Printf("generated settlement for live_session=%d status=%s", res.LiveSessionID, res.ReconciliationStatus)
		return
	}

	saramaCfg := sarama.NewConfig()
	saramaCfg.Version = sarama.V4_1_1_0
	saramaCfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	group, err := sarama.NewConsumerGroup(cfg.KafkaBrokers, "settlement-worker-v1", saramaCfg)
	if err != nil {
		log.Fatalf("new consumer group: %v", err)
	}
	defer func() {
		_ = group.Close()
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		_ = group.Close()
	}()

	h := &handler{svc: svc}
	for {
		if err := group.Consume(ctx, []string{"live.session.closed.v1"}, h); err != nil {
			log.Printf("consume error: %v", err)
			time.Sleep(1 * time.Second)
		}
	}
}
