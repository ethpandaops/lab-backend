package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetMetrics() {
	httpRequestsTotal.Reset()
	httpRequestDuration.Reset()
	httpRequestSize.Reset()
	httpResponseSize.Reset()
}

func TestMetricsMiddleware_UsesRoutePatternNotRawURL(t *testing.T) {
	resetMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/{network}/bounds", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Metrics()(mux)

	for _, network := range []string{"mainnet", "sepolia", "holesky"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/"+network+"/bounds", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	expectedRoute := "GET /api/v1/{network}/bounds"
	count := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, expectedRoute, "200"))
	assert.Equal(t, float64(3), count, "all three requests should collapse to one route-pattern series")

	for _, network := range []string{"mainnet", "sepolia", "holesky"} {
		rawURL := "/api/v1/" + network + "/bounds"
		leak := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, rawURL, "200"))
		assert.Equal(t, float64(0), leak, "raw URL %q must never become a metric label", rawURL)
	}
}

func TestMetricsMiddleware_UnmatchedRouteUsesSentinel(t *testing.T) {
	resetMetrics()

	mux := http.NewServeMux()
	handler := Metrics()(mux)

	req := httptest.NewRequest(http.MethodGet, "/this/path/does/not/exist", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)

	count := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, unmatchedRoute, "404"))
	assert.Equal(t, float64(1), count, "unmatched routes should fall back to the sentinel")

	leak := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/this/path/does/not/exist", "404"))
	assert.Equal(t, float64(0), leak, "raw URL must never become a metric label")
}

func TestMetricsMiddleware_CatchAllPatternCollapsesGarbage(t *testing.T) {
	resetMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Metrics()(mux)

	garbagePaths := []string{
		"/.env",
		"/wp-login.php",
		"/$$whyalwaysme@@.php",
		"/%2f%2eAwS%2fCrEdEnTiAlS",
	}
	for _, p := range garbagePaths {
		req := httptest.NewRequest(http.MethodGet, p, http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	count := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, "/", "200"))
	assert.Equal(t, float64(len(garbagePaths)), count, "all garbage URLs should collapse to the catch-all pattern")

	for _, p := range garbagePaths {
		leak := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, p, "200"))
		assert.Equal(t, float64(0), leak, "scanner garbage URL %q must never become a metric label", p)
	}
}

func TestMetricsMiddleware_RequestSizeRecordedAgainstPattern(t *testing.T) {
	resetMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/upload", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		w.WriteHeader(http.StatusAccepted)
	})

	handler := Metrics()(mux)

	body := strings.Repeat("x", 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	count := testutil.CollectAndCount(httpRequestSize, "http_request_size_bytes")
	assert.Equal(t, 1, count, "request size should be observed exactly once against the route pattern")
}

func TestMetricsMiddleware_MultipleRoutesGetDistinctLabels(t *testing.T) {
	resetMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/v1/{network}/bounds", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Metrics()(mux)

	for _, p := range []string{"/health", "/api/v1/config", "/api/v1/mainnet/bounds", "/api/v1/sepolia/bounds"} {
		req := httptest.NewRequest(http.MethodGet, p, http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	assert.Equal(t, float64(1), testutil.ToFloat64(
		httpRequestsTotal.WithLabelValues(http.MethodGet, "GET /health", "200"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		httpRequestsTotal.WithLabelValues(http.MethodGet, "GET /api/v1/config", "200"),
	))
	assert.Equal(t, float64(2), testutil.ToFloat64(
		httpRequestsTotal.WithLabelValues(http.MethodGet, "GET /api/v1/{network}/bounds", "200"),
	))
}

func TestRoutePattern_ReturnsSentinelWhenUnset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", http.NoBody)
	assert.Equal(t, unmatchedRoute, routePattern(req))
}

func TestRoutePattern_ReturnsMatchedPattern(t *testing.T) {
	mux := http.NewServeMux()

	var observed string

	mux.HandleFunc("GET /api/v1/{network}/bounds", func(w http.ResponseWriter, r *http.Request) {
		observed = routePattern(r)

		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mainnet/bounds", http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "GET /api/v1/{network}/bounds", observed)
}
