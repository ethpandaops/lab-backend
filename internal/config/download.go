//nolint:tagliatelle // superior snake-case yo.
package config

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBlockArchiveURL is the public block archive (serves beacon blocks by slot + block root).
	DefaultBlockArchiveURL = "https://block-archiver.analytics.production.platform.ethpandaops.io"
	// DefaultBlobArchivePattern is the public blob archive template; {network} is the network name.
	DefaultBlobArchivePattern = "https://blob-archive.{network}.ethpandaops.io"

	networkPlaceholder = "{network}"
)

// DownloadConfig holds configuration for the beacon block and blob download proxy.
// The proxy streams artifacts from the upstream archives back to the client as
// attachments, sidestepping the archives' inline content disposition and missing
// CORS headers.
type DownloadConfig struct {
	Enabled            bool          `yaml:"enabled"`
	BlockArchiveURL    string        `yaml:"block_archive_url"`    // Base URL of the block archive
	BlobArchivePattern string        `yaml:"blob_archive_pattern"` // Blob archive URL template ({network} placeholder)
	RequestTimeout     time.Duration `yaml:"request_timeout"`      // HTTP request timeout for upstream fetches
}

// Validate validates the download configuration and sets defaults.
func (c *DownloadConfig) Validate() error {
	if c.BlockArchiveURL == "" {
		c.BlockArchiveURL = DefaultBlockArchiveURL
	}

	c.BlockArchiveURL = strings.TrimRight(c.BlockArchiveURL, "/")

	if c.BlobArchivePattern == "" {
		c.BlobArchivePattern = DefaultBlobArchivePattern
	}

	c.BlobArchivePattern = strings.TrimRight(c.BlobArchivePattern, "/")

	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}

	if !c.Enabled {
		return nil
	}

	if c.RequestTimeout < 5*time.Second {
		return fmt.Errorf("request_timeout must be at least 5 seconds, got %v", c.RequestTimeout)
	}

	if !strings.Contains(c.BlobArchivePattern, networkPlaceholder) {
		return fmt.Errorf("blob_archive_pattern must contain the %s placeholder", networkPlaceholder)
	}

	return nil
}

// BlobArchiveURLFor returns the blob archive base URL for a specific network.
func (c *DownloadConfig) BlobArchiveURLFor(network string) string {
	return strings.ReplaceAll(c.BlobArchivePattern, networkPlaceholder, network)
}

// HTTPClient returns a configured HTTP client for upstream archive requests.
func (c *DownloadConfig) HTTPClient() *http.Client {
	return &http.Client{
		Timeout: c.RequestTimeout,
	}
}
