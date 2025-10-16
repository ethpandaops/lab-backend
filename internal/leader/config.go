package leader

import "time"

// Config holds leader election configuration.
type Config struct {
	LockKey       string
	LockTTL       time.Duration
	RenewInterval time.Duration
	RetryInterval time.Duration
}
