// Package config contains runtime configuration wiring.
package config

import (
	"os"
)

// Config contains runtime settings for all binaries.
type Config struct {
	HTTPAddr          string
	MySQLDSN          string
	KafkaBrokers      []string
	RedisAddr         string
	AllowedGiftRegion map[string]struct{}
}

// Load returns config from env with sane local defaults.
func Load() Config {
	cfg := Config{
		HTTPAddr:     getEnv("HTTP_ADDR", ":8080"),
		MySQLDSN:     getEnv("MYSQL_DSN", "root:root@tcp(localhost:3306)/live_revenue?parseTime=true&multiStatements=true"),
		RedisAddr:    getEnv("REDIS_ADDR", "localhost:6379"),
		KafkaBrokers: []string{getEnv("KAFKA_BROKER", "localhost:9092")},
		AllowedGiftRegion: map[string]struct{}{
			"US": {},
			"KR": {},
			"JP": {},
			"GB": {},
			"DE": {},
			"FR": {},
		},
	}
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
