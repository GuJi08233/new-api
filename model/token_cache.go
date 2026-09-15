package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

func getTokenCacheKey(key string) string     { return "token:" + common.GenerateHMAC(key) }
func quotaCacheVersionKey(key string) string { return key + ":version" }

// 版本戳在数据库读取之前取得，变更前后替换，阻止在途旧快照重新水合。
func cacheVersion(key string) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}
	ctx := context.Background()
	versionKey := quotaCacheVersionKey(key)
	value, err := common.RDB.Get(ctx, versionKey).Result()
	if err != redis.Nil {
		return value, err
	}
	// 使用随机版本而非过期后归零，避免旧读者跨过 TTL 后复活快照。
	value = common.GetRandomString(24)
	if err := common.RDB.SetNX(ctx, versionKey, value, quotaCacheVersionTTL()).Err(); err != nil {
		return "", err
	}
	return common.RDB.Get(ctx, versionKey).Result()
}

func quotaCacheVersionTTL() time.Duration {
	ttl := common.RedisKeyCacheSeconds()
	if ttl < 60 {
		ttl = 60
	}
	return time.Duration(ttl+60) * time.Second
}

func invalidateVersionedCache(key string) error {
	if !common.RedisEnabled {
		return nil
	}
	const script = `redis.call('SET', KEYS[2], ARGV[1], 'EX', ARGV[2]); redis.call('DEL', KEYS[1]); return 1`
	return common.RDB.Eval(context.Background(), script, []string{key, quotaCacheVersionKey(key)}, common.GetRandomString(24), int(quotaCacheVersionTTL()/time.Second)).Err()
}

func cacheInitHash(key, version string, fields []interface{}) error {
	if !common.RedisEnabled {
		return nil
	}
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		ttl = 60
	}
	args := []interface{}{version, ttl}
	args = append(args, fields...)
	const script = `
if (redis.call('GET', KEYS[2]) or '') ~= ARGV[1] then return 0 end
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') > 0 then return 0 end
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], unpack(ARGV, 3))
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{key, quotaCacheVersionKey(key)}, args...).Err()
}

func tokenCacheVersion(key string) (string, error) { return cacheVersion(getTokenCacheKey(key)) }

func cacheInitToken(token Token, version string) error {
	allowIPs := ""
	if token.AllowIps != nil {
		allowIPs = *token.AllowIps
	}
	return cacheInitHash(getTokenCacheKey(token.Key), version, []interface{}{
		"Id", token.Id, "UserId", token.UserId, "Status", token.Status, "Name", token.Name,
		"CreatedTime", token.CreatedTime, "AccessedTime", token.AccessedTime, "ExpiredTime", token.ExpiredTime,
		"UnlimitedQuota", strconv.FormatBool(token.UnlimitedQuota), "ModelLimitsEnabled", strconv.FormatBool(token.ModelLimitsEnabled),
		"ModelLimits", token.ModelLimits, "AllowIps", allowIPs, "Group", token.Group,
		"CrossGroupRetry", strconv.FormatBool(token.CrossGroupRetry), "RemainQuota", token.RemainQuota, "UsedQuota", token.UsedQuota,
	})
}

func cacheDeleteToken(key string) error {
	if key == "" {
		return nil
	}
	return invalidateVersionedCache(getTokenCacheKey(key))
}

func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var token Token
	if err := common.RedisHGetObj(getTokenCacheKey(key), &token); err != nil {
		return nil, err
	}
	if token.Id <= 0 {
		return nil, fmt.Errorf("token cache is incomplete")
	}
	token.Key = key
	return &token, nil
}
