package ratelimit

import (
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
