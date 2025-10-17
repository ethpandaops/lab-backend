package redis

//go:generate mockgen -package mocks -destination mocks/mock_client.go github.com/ethpandaops/lab-backend/internal/redis Client

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Compile-time interface compliance check.
var _ Client = (*client)(nil)

// Client provides Redis operations for lab-backend.
type Client interface {
	Start(ctx context.Context) error
	Stop() error
	Ping(ctx context.Context) error
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	GetClient() *redis.Client
}

type client struct {
	log    logrus.FieldLogger
	cfg    Config
	client *redis.Client
}

// NewClient creates a new Redis client.
func NewClient(log logrus.FieldLogger, cfg Config) Client {
	return &client{
		log: log.WithField("component", "redis"),
		cfg: cfg,
	}
}

// Start initializes the Redis connection pool and verifies connectivity.
func (c *client) Start(ctx context.Context) error {
	c.log.WithFields(logrus.Fields{
		"address": c.cfg.Address,
		"db":      c.cfg.DB,
	}).Info("Initializing Redis client")

	c.client = redis.NewClient(&redis.Options{
		Addr:         c.cfg.Address,
		Password:     c.cfg.Password,
		DB:           c.cfg.DB,
		DialTimeout:  c.cfg.DialTimeout,
		ReadTimeout:  c.cfg.ReadTimeout,
		WriteTimeout: c.cfg.WriteTimeout,
		PoolSize:     c.cfg.PoolSize,
	})

	// Verify connection
	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	c.log.Info("Redis client started successfully")

	return nil
}

// Stop closes the Redis connection pool.
func (c *client) Stop() error {
	c.log.Info("Stopping Redis client")

	if c.client != nil {
		return c.client.Close()
	}

	return nil
}

// Ping verifies Redis connectivity.
func (c *client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Get retrieves a value by key.
func (c *client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found: %s", key)
	}

	return val, err
}

// Set stores a key-value pair with optional TTL (0 = no expiration).
func (c *client) Set(
	ctx context.Context,
	key,
	value string,
	ttl time.Duration,
) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Del deletes one or more keys.
func (c *client) Del(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// SetNX sets a key only if it doesn't exist (used for leader election).
// Returns true if the key was set, false if it already existed.
func (c *client) SetNX(
	ctx context.Context,
	key,
	value string,
	ttl time.Duration,
) (bool, error) {
	return c.client.SetNX(ctx, key, value, ttl).Result()
}

// GetClient returns the underlying go-redis client for advanced operations.
func (c *client) GetClient() *redis.Client {
	return c.client
}
