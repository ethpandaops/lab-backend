package bounds

import "time"

// Config holds bounds provider configuration.
type Config struct {
	RefreshInterval time.Duration
	PageSize        int
	BoundsTTL       time.Duration
}
