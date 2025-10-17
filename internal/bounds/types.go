//nolint:tagliatelle // superior snake-case yo.
package bounds

import (
	"context"
	"time"
)

// TableBounds represents the min/max position bounds for a single table.
type TableBounds struct {
	Min int64 `json:"min"` // Minimum position for this table
	Max int64 `json:"max"` // Maximum position + interval for this table
}

// BoundsData represents per-table bounds for a network.
type BoundsData struct {
	Tables      map[string]TableBounds `json:"tables"`       // Map of table name to bounds
	LastUpdated time.Time              `json:"last_updated"` // When this data was last fetched
}

// Provider defines the interface for bounds data providers.
// This abstraction enables future Redis implementation.
type Provider interface {
	Start(ctx context.Context) error
	Stop() error
	GetBounds(ctx context.Context, network string) (*BoundsData, bool)
	GetAllBounds(ctx context.Context) map[string]*BoundsData
	// NotifyChannel returns a channel that signals when bounds data has been updated.
	// Consumers should listen on this channel to refresh cached data.
	NotifyChannel() <-chan struct{}
}

// IncrementalTableRecord represents a single row from admin_cbt_incremental.
type IncrementalTableRecord struct {
	Database        string `json:"database"`
	Table           string `json:"table"`
	Position        int64  `json:"position"`
	Interval        int64  `json:"interval"`
	UpdatedDateTime int64  `json:"updated_date_time"`
}

// AdminCBTIncrementalResponse is the upstream API response wrapper.
type AdminCBTIncrementalResponse struct {
	AdminCBTIncremental []IncrementalTableRecord `json:"admin_cbt_incremental"`
	NextPageToken       string                   `json:"next_page_token"`
}
