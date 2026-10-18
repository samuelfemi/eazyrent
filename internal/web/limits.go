package web

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// limiter is the one-method contract the middleware needs.
// *ratelimit.Limiter satisfies it today; a Redis backend can slot in
// without touching handlers.
type limiter interface {
	Allow(key string) bool
}

// RemoteAddrIP keys callers by the direct peer address. Correct when the
// process is the first hop; behind a proxy it is the proxy, so prefer
// ForwardedIP there (see Handler.ClientIP).
func RemoteAddrIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ForwardedIP keys callers by the client address as reported by a trusted
// proxy: X-Forwarded-For (leftmost, the original client) then X-Real-IP,
// falling back to the peer address. Only safe when a trusted proxy actually
// sets these headers — otherwise clients can spoof them and dodge limits.
func ForwardedIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	return RemoteAddrIP(r)
}

// userKey keys authed routes by user. The route must already sit behind
// RequireAuth; the IP fallback only covers miswiring, keeping the route
// limited instead of open.
func userKey(prefix string) func(*http.Request) string {
	return func(r *http.Request) string {
		if cu, ok := CurrentUserOf(r); ok {
			return prefix + cu.UserID.String()
		}
		return prefix + RemoteAddrIP(r)
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
