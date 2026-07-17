package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"

	"xiuxian/internal/biz"
)

var tokenBucketScript = redis.NewScript(`
local values = redis.call("HMGET", KEYS[1], "tokens", "last")
local now_parts = redis.call("TIME")
local now = tonumber(now_parts[1]) + tonumber(now_parts[2]) / 1000000
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])
local tokens = tonumber(values[1]) or capacity
local last = tonumber(values[2]) or now
if now > last then
  tokens = math.min(capacity, tokens + (now - last) * rate)
end
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call("HSET", KEYS[1], "tokens", tokens, "last", now)
redis.call("PEXPIRE", KEYS[1], ttl)
return allowed
`)

type rateLimiter struct {
	redis  *redis.Client
	memory *memoryRateLimiter
}

func NewRateLimiter(data *Data) biz.RateLimiter {
	return &rateLimiter{redis: data.redis, memory: newMemoryRateLimiter()}
}

func NewMemoryRateLimiter() biz.RateLimiter {
	return &rateLimiter{memory: newMemoryRateLimiter()}
}

func OpenRedisRateLimiter(ctx context.Context, redisURL string, logger log.Logger) (biz.RateLimiter, func(), error) {
	if redisURL == "" {
		return NewMemoryRateLimiter(), func() {}, nil
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, func() {}, err
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, func() {}, err
	}
	cleanup := func() {
		if err := client.Close(); err != nil {
			log.NewHelper(logger).Warnw("operation", "close_rate_limit_redis", "error", err)
		}
	}
	return &rateLimiter{redis: client, memory: newMemoryRateLimiter()}, cleanup, nil
}

func (limiter *rateLimiter) Allow(ctx context.Context, scope, subject string, policy biz.RateLimitPolicy) (bool, error) {
	if policy.RatePerSecond <= 0 || policy.Burst < 1 {
		return false, fmt.Errorf("invalid rate-limit policy")
	}
	if limiter.redis == nil {
		return limiter.memory.allow(scope, subject, policy, time.Now()), nil
	}
	digest := sha256.Sum256([]byte(subject))
	key := "rate:" + scope + ":" + hex.EncodeToString(digest[:])
	result, err := tokenBucketScript.Run(
		ctx,
		limiter.redis,
		[]string{key},
		policy.Burst,
		policy.RatePerSecond,
		max(policy.TTL.Milliseconds(), 1000),
	).Int()
	if err != nil {
		return false, fmt.Errorf("consume rate-limit token: %w", err)
	}
	return result == 1, nil
}

type memoryBucket struct {
	tokens    float64
	last      time.Time
	expiresAt time.Time
}

type memoryRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]memoryBucket
}

func newMemoryRateLimiter() *memoryRateLimiter {
	return &memoryRateLimiter{buckets: make(map[string]memoryBucket)}
}

func (limiter *memoryRateLimiter) allow(scope, subject string, policy biz.RateLimitPolicy, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	for key, bucket := range limiter.buckets {
		if !bucket.expiresAt.After(now) {
			delete(limiter.buckets, key)
		}
	}
	key := scope + "\x00" + subject
	bucket, ok := limiter.buckets[key]
	if !ok {
		bucket = memoryBucket{tokens: policy.Burst, last: now}
	}
	if elapsed := now.Sub(bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens = math.Min(policy.Burst, bucket.tokens+elapsed*policy.RatePerSecond)
		bucket.last = now
	}
	if bucket.tokens < 1 {
		bucket.expiresAt = now.Add(policy.TTL)
		limiter.buckets[key] = bucket
		return false
	}
	bucket.tokens--
	bucket.expiresAt = now.Add(policy.TTL)
	limiter.buckets[key] = bucket
	return true
}
