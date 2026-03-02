// Package main provides the points projector binary.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/mini_station/live-revenue-integrity-lab/internal/config"
	"github.com/mini_station/live-revenue-integrity-lab/internal/db"
	"github.com/mini_station/live-revenue-integrity-lab/internal/events"
	"github.com/mini_station/live-revenue-integrity-lab/internal/projector/points"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type handler struct {
	projector *points.Projector
}

var lagHistogram = prometheus.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "live_revenue_projection_lag_seconds_points",
		Help:    "Kafka consume lag for points projector in seconds.",
		Buckets: prometheus.DefBuckets,
	},
)

func (h *handler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *handler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *handler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if !msg.Timestamp.IsZero() {
			lag := time.Since(msg.Timestamp).Seconds()
			if lag >= 0 {
				lagHistogram.Observe(lag)
			}
		}

		eventID := extractEventID(msg)
		if eventID == 0 {
			session.MarkMessage(msg, "missing_event_id")
			continue
		}

		switch msg.Topic {
		case "live.gift.accepted.v1":
			var evt events.LiveGiftAcceptedV1
			if err := json.Unmarshal(msg.Value, &evt); err != nil {
				session.MarkMessage(msg, "bad_payload")
				continue
			}
			if err := h.projector.ApplyGift(session.Context(), eventID, evt); err != nil {
				return err
			}
		case "live.comment.created.v1":
			var evt events.LiveCommentCreatedV1
			if err := json.Unmarshal(msg.Value, &evt); err != nil {
				session.MarkMessage(msg, "bad_payload")
				continue
			}
			if err := h.projector.ApplyComment(session.Context(), eventID, evt); err != nil {
				return err
			}
		case "live.watch.minute.v1":
			var evt events.LiveWatchMinuteV1
			if err := json.Unmarshal(msg.Value, &evt); err != nil {
				session.MarkMessage(msg, "bad_payload")
				continue
			}
			if err := h.projector.ApplyWatchMinute(session.Context(), eventID, evt); err != nil {
				return err
			}
		}

		session.MarkMessage(msg, "applied")
	}
	return nil
}

func main() {
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
	saramaCfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRange()}
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	group, err := sarama.NewConsumerGroup(cfg.KafkaBrokers, "points-projector-v1", saramaCfg)
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

	h := &handler{projector: points.New(database)}
	prometheus.MustRegister(lagHistogram)
	go startMetricsServer(getEnv("POINTS_METRICS_ADDR", ":9102"))

	topics := []string{"live.gift.accepted.v1", "live.comment.created.v1", "live.watch.minute.v1"}
	for {
		if err := group.Consume(ctx, topics, h); err != nil {
			log.Printf("consume error: %v", err)
			time.Sleep(1 * time.Second)
		}
	}
}

func startMetricsServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Printf("points metrics server stopped: %v", err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func extractEventID(msg *sarama.ConsumerMessage) uint64 {
	for _, h := range msg.Headers {
		if string(h.Key) != "event_id" {
			continue
		}
		v, err := strconv.ParseUint(string(h.Value), 10, 64)
		if err != nil {
			return 0
		}
		return v
	}
	return 0
}
