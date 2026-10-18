package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	idleTTL    = 10 * time.Minute
	maxEntries = 10000
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Limiter is an in-memory per-key token bucket. Keys that fall idle are
// evicted, and the map is hard-capped at max so a caller rotating keys
// (spoofed IPs, many users) cannot grow it without bound.
type Limiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	every    time.Duration
	burst    int
	max      int
}

func NewLimiter(every time.Duration, burst int) *Limiter {
	return &Limiter{visitors: map[string]*visitor{}, every: every, burst: burst, max: maxEntries}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.visitors[key]; !ok && len(l.visitors) >= l.max {
		l.evictLocked()
	}

	v, ok := l.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

// evictLocked frees room for a new key. It first drops entries idle longer
// than idleTTL; if that frees nothing (a burst of fresh keys at the cap) it
// evicts the single least-recently-seen entry so the map never exceeds max.
func (l *Limiter) evictLocked() {
	now := time.Now()
	for k, v := range l.visitors {
		if now.Sub(v.lastSeen) > idleTTL {
			delete(l.visitors, k)
		}
	}
	if len(l.visitors) < l.max {
		return
	}

	var oldestKey string
	var oldest time.Time
	for k, v := range l.visitors {
		if oldestKey == "" || v.lastSeen.Before(oldest) {
			oldestKey, oldest = k, v.lastSeen
		}
	}
	delete(l.visitors, oldestKey)
}

// Limits holds the limiter for each rate-limited surface.
type Limits struct {
	List     *Limiter
	Detail   *Limiter
	Create   *Limiter
	FavWrite *Limiter
	SignUp   *Limiter
	SignIn   *Limiter
	Reset    *Limiter
}

func DefaultLimits() Limits {
	return Limits{
		List:     NewLimiter(time.Minute/30, 30),
		Detail:   NewLimiter(time.Minute/60, 60),
		Create:   NewLimiter(time.Hour/10, 10),
		FavWrite: NewLimiter(time.Minute/60, 60),
		SignUp:   NewLimiter(time.Minute/5, 10),
		SignIn:   NewLimiter(time.Minute/10, 10),
		Reset:    NewLimiter(time.Minute/5, 10),
	}
}
