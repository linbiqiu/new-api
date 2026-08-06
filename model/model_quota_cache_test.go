package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useModelQuotaCacheMiniRedis(t *testing.T) *redis.Client {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})
	return client
}

func TestModelQuotaCacheRoundTripsAmountAndTokens(t *testing.T) {
	useModelQuotaCacheMiniRedis(t)
	want := ModelQuotaUsageSnapshot{QuotaUsed: 12, QuotaLimit: 100, TokenUsed: 34, TokenLimit: 200}
	require.NoError(t, CacheSetModelQuotaUsage(7, want, time.Now().Add(time.Hour).Unix()))

	got, ok, err := CacheGetModelQuotaUsage(7)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, want, got)

	require.NoError(t, CacheIncrModelQuotaUsage(7, 5, 8))
	got, ok, err = CacheGetModelQuotaUsage(7)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ModelQuotaUsageSnapshot{QuotaUsed: 17, QuotaLimit: 100, TokenUsed: 42, TokenLimit: 200}, got)
}

func TestModelQuotaCacheDistinguishesMissFromRedisFailure(t *testing.T) {
	client := useModelQuotaCacheMiniRedis(t)

	got, ok, err := CacheGetModelQuotaUsage(8)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, got)

	require.NoError(t, client.Close())
	got, ok, err = CacheGetModelQuotaUsage(8)
	require.Error(t, err)
	require.False(t, ok)
	require.Zero(t, got)
	require.Error(t, CacheSetModelQuotaUsage(8, ModelQuotaUsageSnapshot{}, time.Now().Add(time.Hour).Unix()))
	require.Error(t, CacheIncrModelQuotaUsage(8, 1, 1))
}

func TestModelQuotaCacheDisabledIsAvailable(t *testing.T) {
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	got, ok, err := CacheGetModelQuotaUsage(9)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, got)
	require.NoError(t, CacheSetModelQuotaUsage(9, ModelQuotaUsageSnapshot{}, time.Now().Add(time.Hour).Unix()))
	require.NoError(t, CacheIncrModelQuotaUsage(9, 1, 1))
}
