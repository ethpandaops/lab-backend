package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_UsesMatchedRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := Metrics()(mux)
	for _, path := range []string{"/items/metric-test-alpha", "/items/metric-test-beta"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString("test")))
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	assertHTTPMetricLabels(t, metricFamilies, http.MethodPost, "POST /items/{id}", "204", []string{
		"/items/metric-test-alpha",
		"/items/metric-test-beta",
	})
}

func TestMetrics_UsesStableLabelForUnmatchedRequests(t *testing.T) {
	handler := Metrics()(http.NewServeMux())
	for _, path := range []string{"/metric-test-missing-a", "/metric-test-missing-b"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString("test")))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	}

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	assertHTTPMetricLabels(t, metricFamilies, http.MethodPatch, "unmatched", "404", []string{
		"/metric-test-missing-a",
		"/metric-test-missing-b",
	})
}

func assertHTTPMetricLabels(
	t *testing.T,
	metricFamilies []*dto.MetricFamily,
	method string,
	path string,
	status string,
	rawPaths []string,
) {
	t.Helper()

	found := map[string]bool{
		"http_requests_total":           false,
		"http_request_duration_seconds": false,
		"http_request_size_bytes":       false,
		"http_response_size_bytes":      false,
	}

	for _, family := range metricFamilies {
		if _, ok := found[family.GetName()]; !ok {
			continue
		}

		for _, metric := range family.GetMetric() {
			labels := labelValues(metric.GetLabel())
			for _, rawPath := range rawPaths {
				assert.NotEqual(t, rawPath, labels["path"], family.GetName())
			}

			if labels["method"] != method || labels["path"] != path {
				continue
			}

			if family.GetName() == "http_requests_total" && labels["status"] != status {
				continue
			}

			found[family.GetName()] = true
		}
	}

	for name, wasFound := range found {
		assert.True(t, wasFound, "expected normalized path label for %s", name)
	}
}

func labelValues(labels []*dto.LabelPair) map[string]string {
	values := make(map[string]string, len(labels))
	for _, label := range labels {
		values[label.GetName()] = label.GetValue()
	}

	return values
}
