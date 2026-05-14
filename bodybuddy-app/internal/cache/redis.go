package cache

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client wraps redis.Client for dependency injection.
type Client struct {
	*redis.Client
}

// New creates a new Redis client.
func New(addr, password string, tlsEnabled bool) *Client {
	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	}
	if tlsEnabled {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	rdb := redis.NewClient(opts)
	return &Client{rdb}
}

// Ping checks Redis connectivity. Satisfies the health.Pinger interface.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}
