package biz

import (
	"context"
	"time"
)

type RateLimitPolicy struct {
	RatePerSecond float64
	Burst         float64
	TTL           time.Duration
}

type RateLimiter interface {
	Allow(context.Context, string, string, RateLimitPolicy) (bool, error)
}

type DependencyHealth struct {
	Postgres string
	Redis    string
}

type DependencyHealthChecker interface {
	Health(context.Context) DependencyHealth
}

var (
	RegistrationRateLimit = RateLimitPolicy{RatePerSecond: 0.2, Burst: 3, TTL: time.Minute}
	LoginRateLimit        = RateLimitPolicy{RatePerSecond: 0.5, Burst: 5, TTL: time.Minute}
	WebSessionRateLimit   = RateLimitPolicy{RatePerSecond: 10, Burst: 20, TTL: time.Minute}
	APIKeyRateLimit       = RateLimitPolicy{RatePerSecond: 1, Burst: 5, TTL: time.Minute}
	MCPToolRateLimit      = RateLimitPolicy{RatePerSecond: 1, Burst: 5, TTL: time.Minute}
)
