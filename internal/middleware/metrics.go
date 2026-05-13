package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// unmatchedRoute is the sentinel value used when a request has no matched
// ServeMux pattern. Collapses unmatched URLs (scanner probes, typos) into a
// single series instead of one series per unique URL.
const unmatchedRoute = "unmatched"

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests. The path label is the matched ServeMux route pattern, not the raw URL.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds. The path label is the matched ServeMux route pattern, not the raw URL.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	httpRequestSize = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "http_request_size_bytes",
			Help: "HTTP request size in bytes. The path label is the matched ServeMux route pattern, not the raw URL.",
		},
		[]string{"method", "path"},
	)

	httpResponseSize = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "http_response_size_bytes",
			Help: "HTTP response size in bytes. The path label is the matched ServeMux route pattern, not the raw URL.",
		},
		[]string{"method", "path"},
	)

	// Rate limiting metrics.
	RateLimitAllowedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_allowed_total",
			Help: "Total number of requests allowed by rate limiter",
		},
		[]string{"rule", "path_pattern"},
	)

	RateLimitDeniedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_denied_total",
			Help: "Total number of requests denied by rate limiter",
		},
		[]string{"rule", "path_pattern"},
	)

	RateLimitErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_errors_total",
			Help: "Total number of rate limiter errors",
		},
		[]string{"error_type"},
	)
)

func init() {
	// Register metrics with the default registry
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(httpRequestSize)
	prometheus.MustRegister(httpResponseSize)
}

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (mrw *metricsResponseWriter) WriteHeader(code int) {
	mrw.statusCode = code
	mrw.ResponseWriter.WriteHeader(code)
}

func (mrw *metricsResponseWriter) Write(b []byte) (int, error) {
	n, err := mrw.ResponseWriter.Write(b)
	mrw.bytesWritten += n

	return n, err
}

// routePattern returns the ServeMux pattern that matched the request, or
// unmatchedRoute if no pattern matched. Using the raw r.URL.Path here would
// give unbounded label cardinality (one series per unique URL).
func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}

	return unmatchedRoute
}

// Metrics returns middleware that collects Prometheus metrics.
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status and bytes
			mrw := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				bytesWritten:   0,
			}

			// Snapshot request size before invoking the chain.
			contentLength := r.ContentLength

			// Call next handler — ServeMux populates r.Pattern during routing.
			next.ServeHTTP(mrw, r)

			// Record metrics
			duration := time.Since(start)
			route := routePattern(r)

			if contentLength > 0 {
				httpRequestSize.WithLabelValues(r.Method, route).Observe(float64(contentLength))
			}

			httpRequestsTotal.WithLabelValues(
				r.Method,
				route,
				strconv.Itoa(mrw.statusCode),
			).Inc()

			httpRequestDuration.WithLabelValues(
				r.Method,
				route,
			).Observe(duration.Seconds())

			httpResponseSize.WithLabelValues(
				r.Method,
				route,
			).Observe(float64(mrw.bytesWritten))
		})
	}
}
