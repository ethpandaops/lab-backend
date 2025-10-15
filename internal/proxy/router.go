package proxy

import (
	"fmt"
	"strings"
)

// ExtractNetwork extracts network name from URL path.
// Path format: /api/v1/{network}/...
// Returns: network name, remaining path, error.
func ExtractNetwork(path string) (network string, remainingPath string, err error) {
	// Split path by "/"
	parts := strings.Split(path, "/")

	// Expected format: ["", "api", "v1", "{network}", ...]
	if len(parts) < 4 {
		return "", "", fmt.Errorf("invalid path format: expected /api/v1/{network}/..., got %s", path)
	}

	if parts[1] != "api" || parts[2] != "v1" {
		return "", "", fmt.Errorf("invalid path format: expected /api/v1/{network}/..., got %s", path)
	}

	network = parts[3]
	if network == "" {
		return "", "", fmt.Errorf("network name cannot be empty")
	}

	// Build remaining path from parts[4:]
	if len(parts) > 4 {
		remainingPath = "/" + strings.Join(parts[4:], "/")
	} else {
		remainingPath = "/"
	}

	return network, remainingPath, nil
}

// RewritePath removes the network segment from path for backend forwarding.
// Input: /api/v1/{network}/fct_block?slot_eq=1000.
// Output: /api/v1/fct_block (query preserved automatically by ReverseProxy).
func RewritePath(path string) (string, error) {
	// Use ExtractNetwork to get remainingPath
	_, remainingPath, err := ExtractNetwork(path)
	if err != nil {
		return "", err
	}

	// Return /api/v1 + remainingPath
	return "/api/v1" + remainingPath, nil
}

// ValidatePath checks if path matches expected format.
func ValidatePath(path string) bool {
	// Check if path starts with /api/v1/
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}

	// Check if path has at least 4 segments
	parts := strings.Split(path, "/")

	return len(parts) >= 4
}
