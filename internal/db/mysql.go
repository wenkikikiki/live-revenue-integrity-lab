// Package db provides DB connection helpers.
package db

import (
	"context"
	"database/sql"
	"time"

	// MySQL driver for database/sql.
	_ "github.com/go-sql-driver/mysql"
)

// OpenMySQL creates a pool and verifies connectivity.
func OpenMySQL(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxIdleTime(2 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetMaxIdleConns(20)
	db.SetMaxOpenConns(60)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
