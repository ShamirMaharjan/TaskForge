package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueryExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)

	if err != nil {
		return nil, fmt.Errorf("failed to parse database configuration string: %w", err)
	}

	cfg.MaxConns = 25
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// pingCtx governs initial connection allocation during pool startup
	pool, err := pgxpool.NewWithConfig(pingCtx, cfg)

	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	// Explicitly verify connection reachability
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database server is unreachable during intial ping: %w", err)
	}

	return pool, nil

}
