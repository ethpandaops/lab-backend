//nolint:tagliatelle // superior snake-case yo.
package version

import (
	"fmt"
	"os"
	"strings"
)

var (
	// These variables are set via ldflags at build time.
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Info contains version information.
type Info struct {
	Version         string `json:"version"`
	GitCommit       string `json:"git_commit"`
	BuildDate       string `json:"build_date"`
	FrontendVersion string `json:"frontend_version,omitempty"`
}

// Get returns version information as a struct.
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
	}
}

// GetWithFrontend returns version information including frontend version.
// It reads the frontend version from .tmp/frontend-version.txt if it exists.
func GetWithFrontend() Info {
	info := Get()
	info.FrontendVersion = readFrontendVersion()

	return info
}

// readFrontendVersion reads the frontend version from .tmp/frontend-version.txt.
// Returns empty string if the file doesn't exist or can't be read.
func readFrontendVersion() string {
	data, err := os.ReadFile(".tmp/frontend-version.txt")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
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
