package frontend

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectConfigAndBounds(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		config      interface{}
		bounds      interface{}
		expectError bool
		errorMsg    string
		contains    []string
	}{
		{
			name:   "valid injection with simple data",
			html:   "<html><head></head><body></body></html>",
			config: map[string]string{"version": "1.0"},
			bounds: map[string]int{"max": 100},
			contains: []string{
				"window.__CONFIG__",
				"window.__BOUNDS__",
				`"version":"1.0"`,
				`"max":100`,
				"<script>",
				"</script>",
			},
		},
		{
			name:   "injection preserves HTML structure",
			html:   "<html><head><title>Test</title></head><body></body></html>",
			config: map[string]string{"test": "data"},
			bounds: map[string]string{},
			contains: []string{
				"<head>",
				"<title>Test</title>",
				"window.__CONFIG__",
				"</head>",
			},
		},
		{
			name:        "missing head tag returns error",
			html:        "<html><body></body></html>",
			config:      map[string]string{},
			bounds:      map[string]string{},
			expectError: true,
			errorMsg:    "could not find <head> tag",
		},
		{
			name:   "empty config and bounds",
			html:   "<html><head></head></html>",
			config: map[string]string{},
			bounds: map[string]string{},
			contains: []string{
				"window.__CONFIG__ = {}",
				"window.__BOUNDS__ = {}",
			},
		},
		{
			name:   "special characters are escaped",
			html:   "<html><head></head></html>",
			config: map[string]string{"name": "</script><script>alert('xss')</script>"},
			bounds: map[string]string{},
			contains: []string{
				`\u003c/script\u003e`, // JSON escapes < and > as Unicode
			},
		},
		{
			name: "complex nested data",
			html: "<html><head></head></html>",
			config: map[string]interface{}{
				"networks": []string{"mainnet", "sepolia"},
				"settings": map[string]bool{"enabled": true},
			},
			bounds: map[string]interface{}{
				"mainnet": map[string]int{"min": 0, "max": 1000},
			},
			contains: []string{
				"window.__CONFIG__",
				"window.__BOUNDS__",
				"networks",
				"mainnet",
			},
		},
		{
			name:        "empty HTML returns error",
			html:        "",
			config:      map[string]string{},
			bounds:      map[string]string{},
			expectError: true,
			errorMsg:    "could not find <head> tag",
		},
		{
			name:   "multiple head tags uses first one",
			html:   "<html><head></head><body><head></head></body></html>",
			config: map[string]string{"test": "data"},
			bounds: map[string]string{},
			contains: []string{
				"window.__CONFIG__",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := InjectConfigAndBounds([]byte(tt.html), tt.config, tt.bounds)

			if tt.expectError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}

				return
			}

			require.NoError(t, err)

			resultStr := string(result)

			for _, expected := range tt.contains {
				assert.Contains(t, resultStr, expected, "expected to find: %s", expected)
			}

			// Verify injection is inside <head> tag
			headStart := strings.Index(resultStr, "<head>")
			headEnd := strings.Index(resultStr, "</head>")
			scriptStart := strings.Index(resultStr, "window.__CONFIG__")

			if headStart != -1 && headEnd != -1 && scriptStart != -1 {
				assert.True(t, scriptStart > headStart && scriptStart < headEnd,
					"script should be injected inside <head> tag")
			}
		})
	}
}
