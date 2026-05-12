package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Client wraps redis.Client for dependency injection.
type Client struct {
	*redis.Client
}

// New creates a new Redis client.
func New(addr, password string) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	return &Client{rdb}
}

// Ping checks Redis connectivity. Satisfies the health.Pinger interface.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}
