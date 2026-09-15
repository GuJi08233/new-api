package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func redisIntegrationClient(t *testing.T) *redis.Client {
	t.Helper()
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR 未设置，跳过真实 Redis 集成测试")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(context.Background()).Err())
	previousClient, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousClient, previousEnabled
		require.NoError(t, client.Close())
	})
	return client
}

// 令牌缓存只保存配置，余额始终来自主库；在途旧快照不能用过期版本号重新水合缓存。
func TestRedisTokenCacheCannotResurrectStaleBalance(t *testing.T) {
	client := redisIntegrationClient(t)
	truncateTables(t)
	key := fmt.Sprintf("redis-token-%d", os.Getpid())
	cacheKey := getTokenCacheKey(key)
	t.Cleanup(func() { _ = client.Del(context.Background(), cacheKey, quotaCacheVersionKey(cacheKey)).Err() })
	require.NoError(t, DB.Create(&Token{UserId: 901, Key: key, RemainQuota: 100, Status: common.TokenStatusEnabled, ExpiredTime: -1}).Error)

	token, err := GetTokenByKey(key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, token.RemainQuota)
	_, err = cacheGetTokenByKey(key)
	require.NoError(t, err, "the first read hydrates the cache")

	staleVersion, err := tokenCacheVersion(key)
	require.NoError(t, err)
	ok, err := TryReserveTokenQuota(token.Id, 901, key, 30)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = cacheGetTokenByKey(key)
	require.Error(t, err, "a reserve invalidates the cached snapshot")

	require.NoError(t, cacheInitToken(*token, staleVersion))
	_, err = cacheGetTokenByKey(key)
	require.Error(t, err, "a snapshot taken before the reserve must not rehydrate the cache")

	for i := 0; i < 2; i++ {
		fresh, err := GetTokenByKey(key, false)
		require.NoError(t, err)
		assert.Equal(t, 70, fresh.RemainQuota, "read %d", i+1)
		assert.Equal(t, 30, fresh.UsedQuota, "read %d", i+1)
	}
	_, err = cacheGetTokenByKey(key)
	assert.NoError(t, err, "the second read is served from the rehydrated cache")
}

// 用户缓存同样只水合冷缓存；预扣后余额来自主库，旧版本号的快照写入被拒绝。
func TestRedisUserCacheServesBalanceFromDatabase(t *testing.T) {
	client := redisIntegrationClient(t)
	truncateTables(t)
	cacheKey := getUserCacheKey(902)
	t.Cleanup(func() { _ = client.Del(context.Background(), cacheKey, quotaCacheVersionKey(cacheKey)).Err() })
	require.NoError(t, DB.Create(&User{Id: 902, Username: "redis-user", Quota: 100}).Error)

	cached, err := GetUserCache(902)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.Quota)
	_, err = cacheGetUserBase(902)
	require.NoError(t, err, "the first read hydrates the cache")

	staleVersion, err := cacheVersion(cacheKey)
	require.NoError(t, err)
	ok, err := TryReserveUserQuota(902, 30)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = cacheGetUserBase(902)
	require.Error(t, err, "a reserve invalidates the cached snapshot")

	require.NoError(t, populateUserCache(User{Id: 902, Username: "redis-user", Quota: 100}, staleVersion))
	_, err = cacheGetUserBase(902)
	require.Error(t, err, "a stale snapshot must not rehydrate the cache")

	for i := 0; i < 2; i++ {
		cached, err = GetUserCache(902)
		require.NoError(t, err)
		assert.Equal(t, 70, cached.Quota, "read %d", i+1)
	}
	_, err = cacheGetUserBase(902)
	assert.NoError(t, err, "the second read is served from the rehydrated cache")
}
