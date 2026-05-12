package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool for dependency injection.
type Pool struct {
	*pgxpool.Pool
}

// New creates a new pgxpool.Pool from the given DSN.
// maxConns should be set to roughly (CPU cores × 2–4) for the target instance.
func New(ctx context.Context, dsn string, maxConns int32) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing db config: %w", err)
	}

	cfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating db pool: %w", err)
	}

	return &Pool{pool}, nil
}

// Ping checks database connectivity. Satisfies the health.Pinger interface.
func (p *Pool) Ping(ctx context.Context) error {
	if err := p.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}
	return nil
}
