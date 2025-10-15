//nolint:tagliatelle // superior snake-case yo.
package version

import (
	"fmt"
)

var (
	// These variables are set via ldflags at build time.
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Info contains version information.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
}

// Get returns version information as a struct.
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
	}
}

// Short returns a short version string.
// Example: "v1.0.0".
func Short() string {
	return Version
}

// Full returns a detailed version string.
// Example: "v1.0.0 (commit: abc123, built: 2024-01-01T00:00:00Z)".
func Full() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildDate)
}
