package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRateLimitIsolatesConfiguredGroupsAndSharesGlobalLimit(t *testing.T) {
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	previousGroups, previousRedisEnabled, previousRedis := setting.ModelRequestRateLimitGroup, common.RedisEnabled, common.RDB
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitGroup, common.RedisEnabled, common.RDB = previousGroups, previousRedisEnabled, previousRedis
	})
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RDB = client
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = true, 1

	for index, tc := range []struct {
		name   string
		redis  bool
		global [2]int
		large  [2]int
		small  [2]int
	}{
		{name: "memory success", global: [2]int{0, 1}, large: [2]int{0, 3}, small: [2]int{0, 1}},
		{name: "redis success", redis: true, global: [2]int{0, 1}, large: [2]int{0, 3}, small: [2]int{0, 1}},
		{name: "memory total", global: [2]int{1, 1000}, large: [2]int{3, 1000}, small: [2]int{1, 1000}},
		{name: "redis total", redis: true, global: [2]int{1, 1000}, large: [2]int{3, 1000}, small: [2]int{1, 1000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.RedisEnabled = tc.redis
			setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = tc.global[0], tc.global[1]
			setting.ModelRequestRateLimitGroup = map[string][2]int{"large": tc.large, "small": tc.small}
			router := gin.New()
			router.GET("/:group", func(c *gin.Context) {
				c.Set("id", 7300000+index)
				common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Param("group"))
			}, ModelRequestRateLimit(), func(c *gin.Context) { c.Status(http.StatusOK) })
			for _, step := range []struct {
				group  string
				status int
			}{
				{"large", http.StatusOK}, {"large", http.StatusOK},
				{"small", http.StatusOK}, {"small", http.StatusTooManyRequests},
				{"large", http.StatusOK}, {"large", http.StatusTooManyRequests},
				{"global-a", http.StatusOK}, {"global-b", http.StatusTooManyRequests},
			} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+step.group, nil))
				assert.Equal(t, step.status, response.Code, "group %s", step.group)
			}
		})
	}
}
