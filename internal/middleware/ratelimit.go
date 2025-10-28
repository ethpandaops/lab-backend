package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/ethpandaops/lab-backend/internal/ratelimit"
)

type compiledRule struct {
	name    string
	pattern *regexp.Regexp
	limit   int
	window  time.Duration
}

// RateLimit returns a middleware that enforces rate limiting.
func RateLimit(
	log logrus.FieldLogger,
	cfg config.RateLimitingConfig,
	limiter ratelimit.Service,
) func(http.Handler) http.Handler {
	// Pre-compile regex patterns for performance
	compiledRules := make([]compiledRule, len(cfg.Rules))
	for i, rule := range cfg.Rules {
		compiledRules[i] = compiledRule{
			name:    rule.Name,
			pattern: regexp.MustCompile(rule.PathPattern),
			limit:   rule.Limit,
			window:  rule.Window,
		}
	}

	// Pre-parse exempt IP ranges
	exemptNets := parseExemptIPs(cfg.ExemptIPs)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP
			ip := extractClientIP(r)

			// Check if IP is whitelisted
			if isExempt(ip, exemptNets) {
				next.ServeHTTP(w, r)

				return
			}

			// Find matching rate limit rule
			rule := findMatchingRule(r.URL.Path, compiledRules)
			if rule == nil {
				// No matching rule, allow request
				next.ServeHTTP(w, r)

				return
			}

			// Check rate limit
			allowed, remaining, resetAt, err := limiter.Allow(r.Context(), ip, rule.name, rule.limit, rule.window)
			if err != nil {
				RateLimitErrorsTotal.WithLabelValues("redis_error").Inc()

				log.WithError(err).WithFields(logrus.Fields{
					"ip":   ip,
					"path": r.URL.Path,
					"rule": rule.name,
				}).Error("rate limit check failed")

				// Error already handled by limiter's failure mode
				if !allowed {
					writeRateLimitError(w, "service unavailable", 0)

					return
				}
			}

			// Set rate limit headers (standard practice)
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rule.limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

			if !allowed {
				// Rate limit exceeded
				RateLimitDeniedTotal.WithLabelValues(rule.name, rule.pattern.String()).Inc()

				retryAfter := int(time.Until(resetAt).Seconds())
				if retryAfter < 0 {
					retryAfter = int(rule.window.Seconds())
				}

				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeRateLimitError(w, "rate limit exceeded", retryAfter)

				log.WithFields(logrus.Fields{
					"ip":          ip,
					"path":        r.URL.Path,
					"rule":        rule.name,
					"retry_after": retryAfter,
				}).Warn("rate limit exceeded")

				return
			}

			// Allowed, continue to next handler
			RateLimitAllowedTotal.WithLabelValues(rule.name, rule.pattern.String()).Inc()
			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the real client IP from the request.
// Priority: X-Forwarded-For (Cloudflare) > X-Real-IP > RemoteAddr.
func extractClientIP(r *http.Request) string {
	// Cloudflare sets CF-Connecting-IP
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}

	// X-Forwarded-For (may contain multiple IPs, take first)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fallback to RemoteAddr (strip port)
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

func parseExemptIPs(exemptIPs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(exemptIPs))

	for _, cidr := range exemptIPs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			if ip := net.ParseIP(cidr); ip != nil {
				// Convert single IP to /32 or /128 CIDR
				if ip.To4() != nil {
					_, network, _ = net.ParseCIDR(cidr + "/32")
				} else {
					_, network, _ = net.ParseCIDR(cidr + "/128")
				}

				nets = append(nets, network)
			}

			continue
		}

		nets = append(nets, network)
	}

	return nets
}

func isExempt(ip string, exemptNets []*net.IPNet) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	for _, network := range exemptNets {
		if network.Contains(parsedIP) {
			return true
		}
	}

	return false
}

func findMatchingRule(path string, rules []compiledRule) *compiledRule {
	for i := range rules {
		if rules[i].pattern.MatchString(path) {
			return &rules[i]
		}
	}

	return nil
}

func writeRateLimitError(w http.ResponseWriter, message string, retryAfter int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests) // 429

	response := map[string]any{
		"error":  message,
		"status": http.StatusTooManyRequests,
	}

	if retryAfter > 0 {
		response["retry_after"] = retryAfter
	}

	_ = json.NewEncoder(w).Encode(response)
}
