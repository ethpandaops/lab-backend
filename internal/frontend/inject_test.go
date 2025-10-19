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

func TestInjectAll(t *testing.T) {
	htmlContent := []byte(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <title>Test</title>
</head>
<body>
  <div id="root"></div>
</body>
</html>`)

	configData := map[string]interface{}{
		"networks": []map[string]interface{}{
			{"name": "mainnet"},
		},
	}

	boundsData := map[string]interface{}{
		"mainnet": map[string]interface{}{
			"min": 0,
			"max": 100,
		},
	}

	t.Run("injects config, bounds, and head raw", func(t *testing.T) {
		headRaw := `<meta property="og:title" content="Test Page">`

		result, err := InjectAll(htmlContent, configData, boundsData, headRaw)
		require.NoError(t, err)

		// Check config injection
		assert.Contains(t, string(result), "window.__CONFIG__")
		assert.Contains(t, string(result), `"networks"`)

		// Check bounds injection
		assert.Contains(t, string(result), "window.__BOUNDS__")
		assert.Contains(t, string(result), `"mainnet"`)

		// Check head raw injection
		assert.Contains(t, string(result), headRaw)

		// Verify head raw is before </head>
		headCloseIdx := strings.Index(string(result), "</head>")
		headRawIdx := strings.Index(string(result), headRaw)
		assert.True(t, headRawIdx < headCloseIdx, "head raw should be before </head>")
	})

	t.Run("injects only config and bounds when headRaw is empty", func(t *testing.T) {
		result, err := InjectAll(htmlContent, configData, boundsData, "")
		require.NoError(t, err)

		// Check config and bounds are injected
		assert.Contains(t, string(result), "window.__CONFIG__")
		assert.Contains(t, string(result), "window.__BOUNDS__")

		// Check structure is maintained
		assert.Contains(t, string(result), "</head>")
		assert.Contains(t, string(result), "</body>")
	})

	t.Run("returns error when no head tag", func(t *testing.T) {
		badHTML := []byte("<html><body></body></html>")

		_, err := InjectAll(badHTML, configData, boundsData, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not find <head> tag")
	})

	t.Run("returns error when no closing head tag", func(t *testing.T) {
		badHTML := []byte("<html><head><body></body></html>")

		_, err := InjectAll(badHTML, configData, boundsData, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not find </head> tag")
	})

	t.Run("escapes script tags in JSON", func(t *testing.T) {
		configWithScript := map[string]interface{}{
			"test": "</script><script>alert('XSS')</script>",
		}

		result, err := InjectAll(htmlContent, configWithScript, boundsData, "")
		require.NoError(t, err)

		// Check that </script> is escaped - Go's JSON encoder uses Unicode escapes
		assert.NotContains(t, string(result), "</script><script>alert('XSS')")
		// The JSON encoder will escape < as \u003c and > as \u003e
		assert.Contains(t, string(result), `\u003c`)
	})
}
