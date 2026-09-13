package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performWebSocketAuthRequest(t *testing.T, loggedIn bool) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("ws-auth-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "tester")
		session.Set("role", common.RoleCommonUser)
		session.Set("id", 7)
		session.Set("status", common.UserStatusEnabled)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/relay", WebSocketUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("id")})
	})

	// A browser WebSocket handshake carries cookies but can never add custom
	// headers, so the request deliberately has no New-Api-User header.
	request := httptest.NewRequest(http.MethodGet, "/relay", nil)
	if loggedIn {
		loginRecorder := httptest.NewRecorder()
		router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
		require.Equal(t, http.StatusNoContent, loginRecorder.Code)
		for _, c := range loginRecorder.Result().Cookies() {
			request.AddCookie(c)
		}
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestWebSocketUserAuthAcceptsSessionWithoutUserIdHeader(t *testing.T) {
	recorder := performWebSocketAuthRequest(t, true)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"id":7`)
}

func TestWebSocketUserAuthRejectsAnonymousHandshake(t *testing.T) {
	recorder := performWebSocketAuthRequest(t, false)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
