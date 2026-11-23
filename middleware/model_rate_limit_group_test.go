package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
	previousTokens, previousConc := setting.ModelRequestRateLimitTokenCount, setting.ModelRequestRateLimitConcurrency
	previousGroups, previousGroupTokens, previousGroupConc := setting.ModelRequestRateLimitGroup, setting.ModelRequestRateLimitGroupTokens, setting.ModelRequestRateLimitGroupConcurrency
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitTokenCount, setting.ModelRequestRateLimitConcurrency = previousTokens, previousConc
		setting.ModelRequestRateLimitGroup, setting.ModelRequestRateLimitGroupTokens, setting.ModelRequestRateLimitGroupConcurrency = previousGroups, previousGroupTokens, previousGroupConc
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRedis
	})
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RDB = client
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = true, 1
	setting.ModelRequestRateLimitTokenCount, setting.ModelRequestRateLimitConcurrency = 0, 0
	setting.ModelRequestRateLimitGroupTokens, setting.ModelRequestRateLimitGroupConcurrency = map[string]int64{}, map[string]int{}

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

func TestModelRateLimitTokenAndConcurrencyLimits(t *testing.T) {
	previousEnabled, previousMinutes := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes
	previousTotal, previousSuccess := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
	previousTokens, previousConc := setting.ModelRequestRateLimitTokenCount, setting.ModelRequestRateLimitConcurrency
	previousGroups, previousGroupTokens, previousGroupConc := setting.ModelRequestRateLimitGroup, setting.ModelRequestRateLimitGroupTokens, setting.ModelRequestRateLimitGroupConcurrency
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousMinutes
		setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount = previousTotal, previousSuccess
		setting.ModelRequestRateLimitTokenCount, setting.ModelRequestRateLimitConcurrency = previousTokens, previousConc
		setting.ModelRequestRateLimitGroup, setting.ModelRequestRateLimitGroupTokens, setting.ModelRequestRateLimitGroupConcurrency = previousGroups, previousGroupTokens, previousGroupConc
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRedis
	})

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RDB = client

	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 1000
	setting.ModelRequestRateLimitTokenCount = 500
	setting.ModelRequestRateLimitConcurrency = 1
	// "legacy" uses 2-element format (defaults TPM=0, conc=0); "limited" sets TPM=100, conc=1.
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"legacy":[0,1000],"limited":[0,1000,100,1],"concGroup":[0,1000,0,1]}`))

	for index, useRedis := range []bool{false, true} {
		name := "memory"
		if useRedis {
			name = "redis"
		}
		t.Run(name, func(t *testing.T) {
			common.RedisEnabled = useRedis
			userID := 7400000 + index

			// 1. TPM enforcement & group isolation
			router := gin.New()
			router.GET("/:group", func(c *gin.Context) {
				c.Set("id", userID)
				common.SetContextKey(c, constant.ContextKeyTokenGroup, c.Param("group"))
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				if target, ok := common.GetContextKeyType[common.ModelRateLimitTokenTarget](c, constant.ContextKeyModelRateLimitTokenTarget); ok {
					common.RecordModelTokenRateLimit(target.Subject, 120, target.Duration)
				}
				c.Status(http.StatusOK)
			})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/limited", nil))
			require.Equal(t, http.StatusOK, rec.Code)

			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/limited", nil))
			assert.Equal(t, http.StatusTooManyRequests, rec.Code, "limited group must block once TPM=100 is exceeded")

			// legacy group has TPM=0 and conc=0, overriding global TPM=500.
			for range 3 {
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy", nil))
				assert.Equal(t, http.StatusOK, rec.Code)
			}

			// Unconfigured group falls back to global TPM=500 (5*120=600 >= 500 blocks 6th).
			for range 5 {
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/global", nil))
				require.Equal(t, http.StatusOK, rec.Code)
			}
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/global", nil))
			assert.Equal(t, http.StatusTooManyRequests, rec.Code, "global group must block at >= 500 tokens")

			// 2. Per-group concurrency enforcement (conc=1 for "concGroup")
			entered := make(chan struct{})
			release := make(chan struct{})
			concRouter := gin.New()
			concRouter.GET("/conc", func(c *gin.Context) {
				c.Set("id", userID+100)
				common.SetContextKey(c, constant.ContextKeyTokenGroup, "concGroup")
			}, ModelRequestRateLimit(), func(c *gin.Context) {
				close(entered)
				<-release
				c.Status(http.StatusOK)
			})

			var firstCode atomic.Int32
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := httptest.NewRecorder()
				concRouter.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/conc", nil))
				firstCode.Store(int32(w.Code))
			}()
			<-entered

			second := httptest.NewRecorder()
			concRouter.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/conc", nil))
			assert.Equal(t, http.StatusTooManyRequests, second.Code, "second concurrent request must be blocked when conc=1")

			close(release)
			wg.Wait()
			assert.EqualValues(t, http.StatusOK, firstCode.Load())
		})
	}
}
