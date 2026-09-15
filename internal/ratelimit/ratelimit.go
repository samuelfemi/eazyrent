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

type Limiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	every    time.Duration
	burst    int
}

func NewLimiter(every time.Duration, burst int) *Limiter {
	return &Limiter{visitors: map[string]*visitor{}, every: every, burst: burst}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.visitors) >= maxEntries {
		now := time.Now()
		for k, v := range l.visitors {
			if now.Sub(v.lastSeen) > idleTTL {
				delete(l.visitors, k)
			}
		}
	}

	v, ok := l.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.visitors[key] = v
	}
	v.lastSeen = time.Now()
	return v.limiter.Allow()
}

type Limits struct {
	List     *Limiter
	Detail   *Limiter
	Create   *Limiter
	FavWrite *Limiter
}

func DefaultLimits() Limits {
	return Limits{
		List:     NewLimiter(time.Minute/30, 30),
		Detail:   NewLimiter(time.Minute/60, 60),
		Create:   NewLimiter(time.Hour/10, 10),
		FavWrite: NewLimiter(time.Minute/60, 60),
	}
}
