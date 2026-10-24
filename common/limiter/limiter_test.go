package limiter

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowRecoversAfterScriptFlushWithoutResettingBucket(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	ctx := context.Background()
	sha, err := client.ScriptLoad(ctx, rateLimitScript).Result()
	require.NoError(t, err)
	limiter := &RedisLimiter{client: client, limitScriptSHA: sha}
	options := []Option{WithCapacity(1000), WithRate(1), WithRequested(1000)}

	allowed, err := limiter.Allow(ctx, "occupied", options...)
	require.NoError(t, err)
	assert.True(t, allowed)
	require.NoError(t, client.ScriptFlush(ctx).Err())

	// 清空脚本缓存不能重置已消费的额度，也不应使该请求变成服务错误。
	allowed, err = limiter.Allow(ctx, "occupied", options...)
	require.NoError(t, err)
	assert.False(t, allowed)
	allowed, err = limiter.Allow(ctx, "unused", options...)
	require.NoError(t, err)
	assert.True(t, allowed)
	allowed, err = limiter.Allow(ctx, "unused", options...)
	require.NoError(t, err)
	assert.False(t, allowed)
}
