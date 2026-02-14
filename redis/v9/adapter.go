package redisv9

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Wrapper for go-redis/v9
type Adapter struct {
	client *redis.Client
}

func New(client *redis.Client) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Get(ctx context.Context, key string) ([]byte, error) {
	return a.client.Get(ctx, key).Bytes()
}

func (a *Adapter) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return a.client.Set(ctx, key, value, expiration).Err()
}

func (a *Adapter) Del(ctx context.Context, key string) error {
	return a.client.Del(ctx, key).Err()
}

func (a *Adapter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return a.client.TTL(ctx, key).Result()
}
