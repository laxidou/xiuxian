package data

import (
	"testing"
	"time"

	"xiuxian/internal/biz"
)

func TestMemoryRateLimiterExpiresIdleBuckets(t *testing.T) {
	limiter := newMemoryRateLimiter()
	policy := biz.RateLimitPolicy{RatePerSecond: 0.001, Burst: 1, TTL: time.Second}
	now := time.Unix(1_700_000_000, 0)

	if !limiter.allow("login", "first", policy, now) {
		t.Fatal("first request should consume the initial token")
	}
	if limiter.allow("login", "first", policy, now) {
		t.Fatal("second immediate request should be limited")
	}
	if !limiter.allow("login", "second", policy, now.Add(2*time.Second)) {
		t.Fatal("new subject should be allowed")
	}
	if _, ok := limiter.buckets["login\x00first"]; ok {
		t.Fatal("expired bucket was not evicted")
	}
	if !limiter.allow("login", "first", policy, now.Add(2*time.Second)) {
		t.Fatal("expired subject should receive a fresh bucket")
	}
}
