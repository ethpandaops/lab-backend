package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/sirupsen/logrus"
)

// Recovery returns middleware that recovers from panics.
func Recovery(logger logrus.FieldLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace
					logger.WithFields(logrus.Fields{
						"error":       fmt.Sprintf("%v", err),
						"stack":       string(debug.Stack()),
						"method":      r.Method,
						"path":        r.URL.Path,
						"remote_addr": r.RemoteAddr,
					}).Error("Panic recovered")

					// Return 500 Internal Server Error
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
