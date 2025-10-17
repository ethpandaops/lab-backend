package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractNetwork(t *testing.T) {
	tests := []struct {
		name              string
		path              string
		expectedNetwork   string
		expectedRemaining string
		expectError       bool
		errorContains     string
	}{
		{
			name:              "valid path extracts network and remaining path",
			path:              "/api/v1/mainnet/bounds",
			expectedNetwork:   "mainnet",
			expectedRemaining: "/bounds",
			expectError:       false,
		},
		{
			name:              "valid path with multiple segments",
			path:              "/api/v1/sepolia/fct_block/summary",
			expectedNetwork:   "sepolia",
			expectedRemaining: "/fct_block/summary",
			expectError:       false,
		},
		{
			name:              "valid path with no remaining path",
			path:              "/api/v1/mainnet",
			expectedNetwork:   "mainnet",
			expectedRemaining: "/",
			expectError:       false,
		},
		{
			name:              "valid path with trailing slash",
			path:              "/api/v1/mainnet/",
			expectedNetwork:   "mainnet",
			expectedRemaining: "/",
			expectError:       false,
		},
		{
			name:              "missing network returns error",
			path:              "/api/v1/",
			expectedNetwork:   "",
			expectedRemaining: "",
			expectError:       true,
			errorContains:     "network name cannot be empty",
		},
		{
			name:          "invalid prefix returns error",
			path:          "/api/bounds",
			expectError:   true,
			errorContains: "invalid path format",
		},
		{
			name:          "empty path returns error",
			path:          "",
			expectError:   true,
			errorContains: "invalid path format",
		},
		{
			name:          "path with only slash",
			path:          "/",
			expectError:   true,
			errorContains: "invalid path format",
		},
		{
			name:          "path missing v1 segment",
			path:          "/api/mainnet/bounds",
			expectError:   true,
			errorContains: "invalid path format",
		},
		{
			name:              "path with hyphens in network name",
			path:              "/api/v1/ethereum-mainnet/bounds",
			expectedNetwork:   "ethereum-mainnet",
			expectedRemaining: "/bounds",
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, remaining, err := ExtractNetwork(tt.path)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedNetwork, network)
				assert.Equal(t, tt.expectedRemaining, remaining)
			}
		})
	}
}

func TestRewritePath(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expected      string
		expectError   bool
		errorContains string
	}{
		{
			name:        "rewrites path correctly",
			input:       "/api/v1/mainnet/bounds",
			expected:    "/api/v1/bounds",
			expectError: false,
		},
		{
			name:        "rewrites path with multiple segments",
			input:       "/api/v1/sepolia/fct_block/summary",
			expected:    "/api/v1/fct_block/summary",
			expectError: false,
		},
		{
			name:        "rewrites path with no remaining path",
			input:       "/api/v1/mainnet",
			expected:    "/api/v1/",
			expectError: false,
		},
		{
			name:        "rewrites path with trailing slash",
			input:       "/api/v1/mainnet/",
			expected:    "/api/v1/",
			expectError: false,
		},
		{
			name:          "invalid path returns error",
			input:         "/api/bounds",
			expectError:   true,
			errorContains: "invalid path format",
		},
		{
			name:          "empty path returns error",
			input:         "",
			expectError:   true,
			errorContains: "invalid path format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RewritePath(tt.input)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "valid path returns true",
			path:     "/api/v1/mainnet/bounds",
			expected: true,
		},
		{
			name:     "valid path with multiple segments returns true",
			path:     "/api/v1/sepolia/fct_block/summary",
			expected: true,
		},
		{
			name:     "valid path with no remaining path returns true",
			path:     "/api/v1/mainnet",
			expected: true,
		},
		{
			name:     "path without /api/v1/ prefix returns false",
			path:     "/api/bounds",
			expected: false,
		},
		{
			name:     "path with only three segments returns false",
			path:     "/api/v1",
			expected: false,
		},
		{
			name:     "empty path returns false",
			path:     "",
			expected: false,
		},
		{
			name:     "path with only slash returns false",
			path:     "/",
			expected: false,
		},
		{
			name:     "path missing version segment returns false",
			path:     "/api/mainnet/bounds",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
