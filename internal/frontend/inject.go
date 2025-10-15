package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// InjectConfig injects config JSON into HTML head.
// Finds <head> tag and inserts: <script>window.__CONFIG__={...}</script>.
func InjectConfig(htmlContent []byte, configData interface{}) ([]byte, error) {
	// Serialize config to JSON
	configJSON, err := json.Marshal(configData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Escape for script tag safety (prevent </script> injection)
	// Replace </ with <\/ to prevent premature script tag closure
	safeJSON := strings.ReplaceAll(string(configJSON), "</", `<\/`)

	// Create script tag
	scriptTag := fmt.Sprintf("\n  <script>\n    window.__CONFIG__ = %s;\n  </script>\n", safeJSON)

	// Find <head> tag and insert script after it
	headTag := []byte("<head>")
	headIndex := bytes.Index(htmlContent, headTag)

	if headIndex == -1 {
		return nil, fmt.Errorf("could not find <head> tag in HTML")
	}

	// Insert script after <head>
	insertPos := headIndex + len(headTag)
	result := make([]byte, 0, len(htmlContent)+len(scriptTag))
	result = append(result, htmlContent[:insertPos]...)
	result = append(result, []byte(scriptTag)...)
	result = append(result, htmlContent[insertPos:]...)

	return result, nil
}
