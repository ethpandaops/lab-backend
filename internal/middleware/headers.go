package middleware

import (
	"net/http"

	"github.com/ethpandaops/lab-backend/internal/headers"
	"github.com/sirupsen/logrus"
)

// Headers returns an HTTP middleware that applies headers based on configured policies.
// The middleware matches the request path against configured patterns and sets
// all headers from the first matching policy.
func Headers(manager *headers.Manager, log logrus.FieldLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Match path to policy and get headers
			matchedHeaders := manager.Match(r.URL.Path)

			if len(matchedHeaders) > 0 {
				// Set all headers from policy
				for key, value := range matchedHeaders {
					w.Header().Set(key, value)
				}

				log.WithFields(logrus.Fields{
					"path":    r.URL.Path,
					"headers": matchedHeaders,
				}).Debug("applied header policy")
			}

			next.ServeHTTP(w, r)
		})
	}
}
