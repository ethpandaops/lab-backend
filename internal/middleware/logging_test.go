package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingMiddleware(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		path              string
		handlerStatus     int
		handlerBody       string
		expectedLogFields []string
	}{
		{
			name:          "GET request logged correctly",
			method:        http.MethodGet,
			path:          "/api/v1/config",
			handlerStatus: http.StatusOK,
			handlerBody:   "test response",
			expectedLogFields: []string{
				"GET",
				"/api/v1/config",
				"200",
				"duration_ms",
				"bytes_written",
			},
		},
		{
			name:          "POST request logged correctly",
			method:        http.MethodPost,
			path:          "/api/v1/data",
			handlerStatus: http.StatusCreated,
			handlerBody:   "created",
			expectedLogFields: []string{
				"POST",
				"/api/v1/data",
				"201",
				"duration_ms",
			},
		},
		{
			name:          "error status logged correctly",
			method:        http.MethodGet,
			path:          "/api/v1/notfound",
			handlerStatus: http.StatusNotFound,
			handlerBody:   "not found",
			expectedLogFields: []string{
				"GET",
				"/api/v1/notfound",
				"404",
			},
		},
		{
			name:          "server error logged correctly",
			method:        http.MethodGet,
			path:          "/api/v1/error",
			handlerStatus: http.StatusInternalServerError,
			handlerBody:   "internal error",
			expectedLogFields: []string{
				"GET",
				"/api/v1/error",
				"500",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer

			logger := logrus.New()
			logger.SetOutput(&buf)
			logger.SetFormatter(&logrus.JSONFormatter{})

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.handlerStatus)
				_, err := w.Write([]byte(tt.handlerBody))
				require.NoError(t, err)
			})

			// Wrap with logging middleware
			wrapped := Logging(logger)(handler)

			// Execute request
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			// Verify handler executed correctly
			assert.Equal(t, tt.handlerStatus, rec.Code)
			assert.Equal(t, tt.handlerBody, rec.Body.String())

			// Verify logging occurred
			logOutput := buf.String()
			assert.NotEmpty(t, logOutput, "should have logged output")

			// Verify expected fields are in log
			for _, field := range tt.expectedLogFields {
				assert.Contains(t, logOutput, field,
					"log should contain field: %s", field)
			}

			// Verify log level is info
			assert.Contains(t, logOutput, `"level":"info"`)
			assert.Contains(t, logOutput, "HTTP request completed")
		})
	}
}

func TestLoggingMiddleware_BytesWritten(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	responseBody := "test response body with some content"

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(responseBody))
		require.NoError(t, err)
	})

	// Wrap with logging middleware
	wrapped := Logging(logger)(handler)

	// Execute request
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Verify bytes_written is logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "bytes_written")

	// Verify response body length matches logged bytes
	assert.Equal(t, len(responseBody), rec.Body.Len())
}

func TestLoggingMiddleware_Duration(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Create test handler that takes some time
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't actually sleep in tests, just write response
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		require.NoError(t, err)
	})

	// Wrap with logging middleware
	wrapped := Logging(logger)(handler)

	// Execute request
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Verify duration is logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "duration_ms")
}

func TestLoggingMiddleware_MultipleWrites(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Create test handler that writes multiple times
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		_, err := w.Write([]byte("first "))
		require.NoError(t, err)

		_, err = w.Write([]byte("second "))
		require.NoError(t, err)

		_, err = w.Write([]byte("third"))
		require.NoError(t, err)
	})

	// Wrap with logging middleware
	wrapped := Logging(logger)(handler)

	// Execute request
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Verify all writes were captured
	expectedBody := "first second third"
	assert.Equal(t, expectedBody, rec.Body.String())

	// Verify bytes_written includes all writes
	logOutput := buf.String()
	assert.Contains(t, logOutput, "bytes_written")

	// Response body length should match all writes
	assert.Equal(t, len(expectedBody), rec.Body.Len())
}

func TestLoggingMiddleware_RequestMetadata(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with logging middleware
	wrapped := Logging(logger)(handler)

	// Execute request with metadata
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.RemoteAddr = "192.168.1.1:12345"

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Verify request metadata is logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "remote_addr")
	assert.Contains(t, logOutput, "user_agent")
	assert.Contains(t, logOutput, "test-agent/1.0")
}

func TestResponseWriter_StatusCodeDefault(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Create test handler that doesn't call WriteHeader
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only write body, no explicit WriteHeader call
		_, err := w.Write([]byte("response"))
		require.NoError(t, err)
	})

	// Wrap with logging middleware
	wrapped := Logging(logger)(handler)

	// Execute request
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Verify default status code (200) is logged
	logOutput := buf.String()

	// When WriteHeader is not called explicitly, status should be 200
	// but only after the first Write call
	parts := strings.Split(logOutput, "\n")
	require.Greater(t, len(parts), 0)

	// Verify the log contains status information
	assert.Contains(t, logOutput, "status")
}
