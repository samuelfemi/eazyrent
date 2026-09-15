package web

import (
	"net"
	"net/http"
	"strconv"
)

// limiter is the one-method contract the middleware needs.
// *ratelimit.Limiter satisfies it today; a Redis backend can slot in
// without touching handlers.
type limiter interface {
	Allow(key string) bool
}

// clientIP keys public routes by caller. RemoteAddr is the direct peer;
// behind a proxy that is the proxy, which is fine until forwarded-header
// handling is actually needed.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// userKey keys authed routes by user. The route must already sit behind
// RequireAuth; the IP fallback only covers miswiring, keeping the route
// limited instead of open.
func userKey(prefix string) func(*http.Request) string {
	return func(r *http.Request) string {
		if cu, ok := CurrentUserOf(r); ok {
			return prefix + cu.UserID.String()
		}
		return prefix + clientIP(r)
	}
}

// limit rejects over-budget callers with 429 and a Retry-After hint.
func limit(l limiter, key func(*http.Request) string, retryAfter int, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(key(r)) {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	}
}
