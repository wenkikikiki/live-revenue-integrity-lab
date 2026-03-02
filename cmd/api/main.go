// Package main provides the API ingress binary.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apiserver "github.com/mini_station/live-revenue-integrity-lab/internal/app/api"
	"github.com/mini_station/live-revenue-integrity-lab/internal/config"
	"github.com/mini_station/live-revenue-integrity-lab/internal/db"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	sqlDB, err := db.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer func() {
		_ = sqlDB.Close()
	}()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() {
		_ = redisClient.Close()
	}()

	srv := apiserver.New(cfg.HTTPAddr, sqlDB, redisClient)
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("api serve: %v", err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
