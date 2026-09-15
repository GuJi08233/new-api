package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisSecurityRateLimitWindow(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR 未设置，跳过真实 Redis Lua 集成测试")
	}
	previous := common.RDB
	client := redis.NewClient(&redis.Options{Addr: address})
	common.RDB = client
	key := "rateLimit:v2:security:" + fmt.Sprint(os.Getpid()) + ":" + t.Name()
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
		common.RDB = previous
		require.NoError(t, client.Close())
	})
	for i, expected := range []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		userRedisRateLimiter(ctx, 2, 60, key)
		assert.Equal(t, expected, recorder.Code, "请求 %d", i+1)
		if expected == http.StatusTooManyRequests {
			assert.Equal(t, "60", recorder.Header().Get("Retry-After"))
		}
	}
	require.NoError(t, client.Del(context.Background(), key).Err())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	userRedisRateLimiter(ctx, 2, 60, key)
	assert.Equal(t, http.StatusOK, recorder.Code)
}
