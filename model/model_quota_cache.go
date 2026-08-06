package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// modelQuotaUsageCacheFields is the Redis hash cache struct
type modelQuotaUsageCacheFields struct {
	QuotaUsed  int64 `json:"quota_used"`
	QuotaLimit int64 `json:"quota_limit"`
	TokenUsed  int64 `json:"token_used"`
	TokenLimit int64 `json:"token_limit"`
	PeriodEnd  int64 `json:"period_end"`
}

type ModelQuotaUsageSnapshot struct {
	QuotaUsed  int64
	QuotaLimit int64
	TokenUsed  int64
	TokenLimit int64
}

func modelQuotaUsageCacheKey(usageId int) string {
	return fmt.Sprintf("model_quota_usage:%d", usageId)
}

// CacheIncrModelQuotaUsage atomically increments both usage metrics in Redis.
func CacheIncrModelQuotaUsage(usageId int, quotaDelta, tokenDelta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	key := modelQuotaUsageCacheKey(usageId)
	return common.RDB.Eval(context.Background(), `
		if redis.call('PTTL', KEYS[1]) > 0 then
			redis.call('HINCRBY', KEYS[1], 'QuotaUsed', ARGV[1])
			redis.call('HINCRBY', KEYS[1], 'TokenUsed', ARGV[2])
			return 1
		end
		return 0
	`, []string{key}, quotaDelta, tokenDelta).Err()
}

// CacheGetModelQuotaUsage reads both usage metrics from Redis cache.
func CacheGetModelQuotaUsage(usageId int) (ModelQuotaUsageSnapshot, bool, error) {
	if !common.RedisEnabled {
		return ModelQuotaUsageSnapshot{}, false, nil
	}
	key := modelQuotaUsageCacheKey(usageId)
	fields, err := common.RDB.HGetAll(context.Background(), key).Result()
	if err != nil {
		return ModelQuotaUsageSnapshot{}, false, fmt.Errorf("get model quota usage cache: %w", err)
	}
	if len(fields) == 0 {
		return ModelQuotaUsageSnapshot{}, false, nil
	}
	for _, requiredField := range []string{"QuotaUsed", "QuotaLimit", "TokenUsed", "TokenLimit"} {
		if _, exists := fields[requiredField]; !exists {
			return ModelQuotaUsageSnapshot{}, false, nil
		}
	}

	var snapshot ModelQuotaUsageSnapshot
	values := map[string]*int64{
		"QuotaUsed":  &snapshot.QuotaUsed,
		"QuotaLimit": &snapshot.QuotaLimit,
		"TokenUsed":  &snapshot.TokenUsed,
		"TokenLimit": &snapshot.TokenLimit,
	}
	for field, target := range values {
		value, exists := fields[field]
		if !exists {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return ModelQuotaUsageSnapshot{}, false, fmt.Errorf("parse model quota usage cache field %s: %w", field, err)
		}
		*target = parsed
	}
	return snapshot, true, nil
}

// CacheSetModelQuotaUsage initializes the Redis cache for a usage record with TTL
func CacheSetModelQuotaUsage(usageId int, snapshot ModelQuotaUsageSnapshot, periodEnd int64) error {
	if !common.RedisEnabled {
		return nil
	}
	key := modelQuotaUsageCacheKey(usageId)
	ttl := time.Duration(periodEnd-time.Now().Unix()) * time.Second
	if ttl <= 0 {
		return nil
	}
	fields := &modelQuotaUsageCacheFields{
		QuotaUsed:  snapshot.QuotaUsed,
		QuotaLimit: snapshot.QuotaLimit,
		TokenUsed:  snapshot.TokenUsed,
		TokenLimit: snapshot.TokenLimit,
		PeriodEnd:  periodEnd,
	}
	return common.RedisHSetObj(key, fields, ttl)
}

// CacheDeleteModelQuotaUsage removes the Redis cache for a usage record
func CacheDeleteModelQuotaUsage(usageId int) {
	if !common.RedisEnabled {
		return
	}
	_ = common.RedisDel(modelQuotaUsageCacheKey(usageId))
}

// cacheGetModelQuotaUsedRaw reads just the quota_used field as a string (used for debugging)
func cacheGetModelQuotaUsedRaw(usageId int) (string, error) {
	if !common.RedisEnabled {
		return "", fmt.Errorf("redis not enabled")
	}
	key := modelQuotaUsageCacheKey(usageId)
	ctx := context.Background()
	val, err := common.RDB.HGet(ctx, key, "QuotaUsed").Result()
	if err != nil {
		return "", err
	}
	return val, nil
}

// parseInt64 helper
func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
