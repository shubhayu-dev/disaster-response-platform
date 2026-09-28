// Package cache defines a minimal cache abstraction and its Redis implementation.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const opTimeout = 500 * time.Millisecond

// Cache is a byte-oriented key/value cache with TTLs.
type Cache interface {
	// Get returns (value, true, nil) on a hit and (nil, false, nil) on a miss.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// Redis implements Cache using go-redis.
type Redis struct {
	client *redis.Client
}

// NewRedis builds a client from a redis:// URL. Connections are lazy, so an
// unavailable Redis never prevents startup; calls simply return errors.
func NewRedis(url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &Redis{client: redis.NewClient(opt)}, nil
}

// Close closes the underlying client.
func (r *Redis) Close() error { return r.client.Close() }

// Get implements Cache.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	val, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

// Set implements Cache.
func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete implements Cache.
func (r *Redis) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return r.client.Del(ctx, key).Err()
}
