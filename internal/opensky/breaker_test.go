package opensky_test

import (
	"testing"
	"time"

	"wroclaw-sky/internal/opensky"
)

func TestBreaker(t *testing.T) {
	t.Parallel()
	b := opensky.NewBreaker(2, 50*time.Millisecond)
	if !b.Allow() || b.Open() {
		t.Fatal("fresh")
	}
	b.Failure()
	if b.Open() {
		t.Fatal("not open yet")
	}
	b.Failure()
	if !b.Open() || b.Allow() {
		t.Fatal("should be open")
	}
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("half-open allow")
	}
	b.Success()
	if b.Open() {
		t.Fatal("reset")
	}
	// defaults
	b2 := opensky.NewBreaker(0, 0)
	b2.Failure()
	b2.Failure()
	b2.Failure()
	if !b2.Open() {
		t.Fatal("default threshold")
	}
	var nilB *opensky.Breaker
	if !nilB.Allow() || nilB.Open() {
		t.Fatal("nil breaker")
	}
	nilB.Success()
	nilB.Failure()
}
