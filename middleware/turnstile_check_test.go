package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnstileCheckPerRequestRequiresFreshTokenDespiteApprovedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevEnabled := common.TurnstileCheckEnabled
	prevVerify := verifyTurnstileFunc
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = prevEnabled
		verifyTurnstileFunc = prevVerify
	})

	var calls, sends atomic.Int32
	var mu sync.Mutex
	used := map[string]bool{}
	verifyTurnstileFunc = func(token string, _ string) error {
		calls.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if token == "expired" || used[token] {
			return service.ErrTurnstileRejected
		}
		used[token] = true
		return nil
	}

	r := gin.New()
	r.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-only-cookie-signing-secret"))))
	r.GET("/seed", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("turnstile", true)
		require.NoError(t, s.Save())
		c.Status(http.StatusNoContent)
	})
	handler := func(c *gin.Context) {
		sends.Add(1)
		c.Status(http.StatusAccepted)
	}
	r.GET("/verification", TurnstileCheckPerRequest(), handler)
	r.GET("/reset_password", TurnstileCheckPerRequest(), handler)
	r.GET("/ordinary", TurnstileCheck(), func(c *gin.Context) { c.Status(http.StatusAccepted) })

	seed := httptest.NewRecorder()
	r.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/seed", nil))
	sessionCookie := seed.Result().Cookies()[0]

	doRequest := func(path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path+"?turnstile="+url.QueryEscape(token), nil)
		req.AddCookie(sessionCookie)
		r.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{"/verification", "/reset_password"} {
		w := doRequest(path, "")
		assert.NotEqual(t, http.StatusAccepted, w.Code)
		assert.Contains(t, w.Body.String(), `"success":false`)
	}
	require.Zero(t, calls.Load())
	require.Zero(t, sends.Load())

	// Ordinary routes still honor the session-cached verification.
	require.Equal(t, http.StatusAccepted, doRequest("/ordinary", "").Code)
	require.Zero(t, calls.Load())

	// Email routes require a fresh, single-use token every time.
	require.Equal(t, http.StatusAccepted, doRequest("/verification", "fresh-one").Code)
	assert.Contains(t, doRequest("/verification", "fresh-one").Body.String(), `"success":false`)
	assert.Contains(t, doRequest("/reset_password", "fresh-one").Body.String(), `"success":false`)
	assert.Contains(t, doRequest("/verification", "expired").Body.String(), `"success":false`)
	require.Equal(t, http.StatusAccepted, doRequest("/reset_password", "fresh-two").Code)
	assert.Equal(t, int32(2), sends.Load())
	assert.Equal(t, int32(5), calls.Load())
}

func TestTurnstileCheckPerRequestHonorsDisabledSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevEnabled := common.TurnstileCheckEnabled
	prevVerify := verifyTurnstileFunc
	common.TurnstileCheckEnabled = false
	verifyTurnstileFunc = func(string, string) error {
		t.Fatal("unexpected verify call when turnstile is disabled")
		return nil
	}
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = prevEnabled
		verifyTurnstileFunc = prevVerify
	})

	r := gin.New()
	r.GET("/verification", TurnstileCheckPerRequest(), func(c *gin.Context) { c.Status(http.StatusAccepted) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/verification", nil))
	require.Equal(t, http.StatusAccepted, w.Code)
}
