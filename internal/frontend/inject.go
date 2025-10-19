package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// InjectConfigAndBounds injects both config and bounds JSON into HTML head in a single script tag.
// Finds <head> tag and inserts: <script>window.__CONFIG__={...}; window.__BOUNDS__={...};</script>.
func InjectConfigAndBounds(htmlContent []byte, configData interface{}, boundsData interface{}) ([]byte, error) {
	// Serialize config to JSON
	configJSON, err := json.Marshal(configData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Serialize bounds to JSON
	boundsJSON, err := json.Marshal(boundsData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bounds: %w", err)
	}

	// Escape for script tag safety (prevent </script> injection)
	// Replace </ with <\/ to prevent premature script tag closure
	safeConfigJSON := strings.ReplaceAll(string(configJSON), "</", `<\/`)
	safeBoundsJSON := strings.ReplaceAll(string(boundsJSON), "</", `<\/`)

	// Create combined script tag with both config and bounds
	scriptTag := fmt.Sprintf(
		"\n    <script>\n      window.__CONFIG__ = %s;\n      window.__BOUNDS__ = %s;\n    </script>\n",
		safeConfigJSON,
		safeBoundsJSON,
	)

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

// InjectAll injects config, bounds, and route-specific head HTML into the HTML head.
// This inserts both the script tag with window.__CONFIG__ and window.__BOUNDS__,
// and the raw head HTML for the specific route.
func InjectAll(htmlContent []byte, configData interface{}, boundsData interface{}, headRaw string) ([]byte, error) {
	// First inject config and bounds
	result, err := InjectConfigAndBounds(htmlContent, configData, boundsData)
	if err != nil {
		return nil, err
	}

	// If no head raw content, return result as is
	if headRaw == "" {
		return result, nil
	}

	// Find where to insert the head raw content
	// We want to insert it after our script tag but still within <head>
	// Find the closing </head> tag and insert before it
	headCloseTag := []byte("</head>")
	headCloseIndex := bytes.Index(result, headCloseTag)

	if headCloseIndex == -1 {
		return nil, fmt.Errorf("could not find </head> tag in HTML")
	}

	// Insert head raw content before </head>
	finalResult := make([]byte, 0, len(result)+len(headRaw))
	finalResult = append(finalResult, result[:headCloseIndex]...)
	finalResult = append(finalResult, []byte("\n    ")...) // Add indentation
	finalResult = append(finalResult, []byte(headRaw)...)
	finalResult = append(finalResult, []byte("\n")...) // Add newline before </head>
	finalResult = append(finalResult, result[headCloseIndex:]...)

	return finalResult, nil
}
