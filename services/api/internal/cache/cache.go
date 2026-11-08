// Package cache creates the Redis client. Postgres remains the source of truth.
package cache

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// New parses the connection URL without contacting Redis.
func New(rawURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL")
	}
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	opts.ContextTimeoutEnabled = true
	opts.MaxRetries = 1
	return redis.NewClient(opts), nil
}
