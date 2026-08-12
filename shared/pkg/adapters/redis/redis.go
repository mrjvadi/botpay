// Package redis implements ports.Cache using go-redis.
// To swap to Memcached: implement ports.Cache using a memcached client and wire in main.go.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/mrjvadi/botpay/shared/pkg/ports"
)

// Cache wraps goredis.UniversalClient and implements ports.Cache.
type Cache struct {
	client goredis.UniversalClient
}

var _ ports.Cache = (*Cache)(nil)

// Config holds Redis connection options.
type Config struct {
	// Standalone mode
	Addr     string
	Password string
	DB       int
	// Sentinel mode (takes precedence if MasterName is set)
	MasterName    string
	SentinelAddrs []string
}

// New connects to Redis and returns a ports.Cache implementation.
func New(cfg Config) (*Cache, error) {
	var client goredis.UniversalClient

	if cfg.MasterName != "" && len(cfg.SentinelAddrs) > 0 {
		client = goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName:    cfg.MasterName,
			SentinelAddrs: cfg.SentinelAddrs,
			Password:      cfg.Password,
			DB:            cfg.DB,
		})
	} else {
		client = goredis.NewClient(&goredis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		})
	}

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping: %w", err)
	}
	return &Cache{client: client}, nil
}

// Client exposes the raw client for adapters that need stream-specific calls.
func (c *Cache) Client() goredis.UniversalClient { return c.client }

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *Cache) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (c *Cache) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return c.client.SetNX(ctx, key, value, ttl).Result()
}

func (c *Cache) BLPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	return c.client.BLPop(ctx, timeout, keys...).Result()
}

func (c *Cache) LPush(ctx context.Context, key string, values ...any) error {
	return c.client.LPush(ctx, key, values...).Err()
}

func (c *Cache) SAdd(ctx context.Context, key string, members ...any) error {
	return c.client.SAdd(ctx, key, members...).Err()
}

func (c *Cache) SIsMember(ctx context.Context, key string, member any) (bool, error) {
	return c.client.SIsMember(ctx, key, member).Result()
}

func (c *Cache) XAdd(ctx context.Context, stream string, values map[string]any) (string, error) {
	return c.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: values,
	}).Result()
}

func (c *Cache) XReadGroup(ctx context.Context, group, consumer, stream string, count int, block time.Duration) ([]ports.StreamMessage, error) {
	msgs, err := c.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    int64(count),
		Block:    block,
	}).Result()
	if err != nil {
		return nil, err
	}
	var out []ports.StreamMessage
	for _, s := range msgs {
		for _, m := range s.Messages {
			out = append(out, ports.StreamMessage{ID: m.ID, Values: m.Values})
		}
	}
	return out, nil
}

func (c *Cache) XAck(ctx context.Context, stream, group string, ids ...string) error {
	return c.client.XAck(ctx, stream, group, ids...).Err()
}

func (c *Cache) XGroupCreateMkStream(ctx context.Context, stream, group, start string) error {
	err := c.client.XGroupCreateMkStream(ctx, stream, group, start).Err()
	if err != nil && err.Error() == "BUSYGROUP Consumer Group name already exists" {
		return nil
	}
	return err
}
