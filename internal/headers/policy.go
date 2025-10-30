package headers

import (
	"fmt"
	"regexp"

	"github.com/ethpandaops/lab-backend/internal/config"
)

// Manager manages header policies and matches request paths to policies.
type Manager struct {
	policies []compiledPolicy
}

// compiledPolicy represents a header policy with a compiled regex pattern.
type compiledPolicy struct {
	name    string
	pattern *regexp.Regexp
	headers map[string]string
}

// NewManager creates a new Manager from a list of header policies.
// Returns an error if any path_pattern is an invalid regex.
func NewManager(policies []config.HeaderPolicy) (*Manager, error) {
	compiled := make([]compiledPolicy, 0, len(policies))

	for _, p := range policies {
		pattern, err := regexp.Compile(p.PathPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid path_pattern in policy %q: %w", p.Name, err)
		}

		compiled = append(compiled, compiledPolicy{
			name:    p.Name,
			pattern: pattern,
			headers: p.Headers,
		})
	}

	return &Manager{policies: compiled}, nil
}

// Match returns headers for the first policy matching the given path.
// Returns nil if no policy matches.
// Policies are evaluated in order - first match wins.
func (m *Manager) Match(path string) map[string]string {
	for _, p := range m.policies {
		if p.pattern.MatchString(path) {
			return p.headers
		}
	}

	return nil
}
