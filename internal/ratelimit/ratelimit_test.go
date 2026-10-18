package ratelimit

import (
	"strconv"
	"testing"
	"time"
)

func TestLimiterBudget(t *testing.T) {
	l := NewLimiter(time.Hour, 2)

	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("first two should be allowed")
	}
	if l.Allow("a") {
		t.Fatal("third should be denied")
	}
	if !l.Allow("b") {
		t.Fatal("keys must be isolated")
	}
}

func TestLimiterRefill(t *testing.T) {
	l := NewLimiter(20*time.Millisecond, 1)

	if !l.Allow("a") {
		t.Fatal("first should be allowed")
	}
	if l.Allow("a") {
		t.Fatal("immediate second should be denied")
	}
	time.Sleep(30 * time.Millisecond)
	if !l.Allow("a") {
		t.Fatal("token should have refilled")
	}
}

// TestLimiterBounded proves that a caller rotating keys cannot grow the map
// past its cap.
func TestLimiterBounded(t *testing.T) {
	l := NewLimiter(time.Hour, 1)

	for i := 0; i < maxEntries*3; i++ {
		l.Allow("key-" + strconv.Itoa(i))
	}

	l.mu.Lock()
	size := len(l.visitors)
	l.mu.Unlock()

	if size > maxEntries {
		t.Fatalf("visitors map must stay bounded, got %d (max %d)", size, maxEntries)
	}
}
