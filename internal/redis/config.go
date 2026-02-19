package redis

import "time"

// Config holds Redis client configuration.
type Config struct {
	Address      string
	Password     string //nolint:gosec // Config field, not a hardcoded secret.
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
}
