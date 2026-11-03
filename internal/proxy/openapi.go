package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
)

const (
	// openAPIPath is the network-scoped spec path after RewritePath strips the network.
	openAPIPath = "/api/v1/openapi.yaml"
	// backendOpenAPIPath is where the backend serves its spec.
	backendOpenAPIPath = "/openapi.yaml"
)

// specPathKey matches the path keys under the spec's paths section, e.g. "    /api/v1/fct_block:".
var specPathKey = regexp.MustCompile(`(?m)^(\s+)/api/v1/`)

// rewriteOpenAPIPaths prefixes the backend spec's /api/v1/<table> paths with the network, so they match the
// /api/v1/{network}/<table> URLs this proxy serves.
func rewriteOpenAPIPaths(r *http.Response, network string) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read openapi spec: %w", err)
	}

	if err := r.Body.Close(); err != nil {
		return fmt.Errorf("close openapi spec: %w", err)
	}

	rewritten := specPathKey.ReplaceAll(body, []byte("${1}/api/v1/"+network+"/"))

	r.Body = io.NopCloser(bytes.NewReader(rewritten))
	r.ContentLength = int64(len(rewritten))
	r.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	r.Header.Del("Content-Encoding")

	return nil
}
