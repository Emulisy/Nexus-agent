// Redis client initialization is shared by refresh-token storage and document queues.
package infra

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
)

// NewRedis builds a client for the selected address, credentials, and logical DB.
// It verifies connectivity with a five-second context before returning the client;
// the caller controls the lifetime of a successfully returned connection pool.
func NewRedis(ctx context.Context, cfg *config.Redis) (*redis.Client, error) {
	cli := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := cli.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return cli, nil
}
