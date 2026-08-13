package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisTrails wraps go-redis for TrailsRedis.
type RedisTrails struct {
	Client *redis.Client
}

func (r *RedisTrails) Get(ctx context.Context, key string) (string, error) {
	if r == nil || r.Client == nil {
		return "", nil
	}
	return r.Client.Get(ctx, key).Result()
}

func (r *RedisTrails) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if r == nil || r.Client == nil {
		return nil
	}
	return r.Client.Set(ctx, key, value, ttl).Err()
}

// NewRedisTrailsFromURL connects to Redis (empty URL → nil).
func NewRedisTrailsFromURL(url string) (*RedisTrails, error) {
	if url == "" {
		return nil, nil
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &RedisTrails{Client: c}, nil
}
