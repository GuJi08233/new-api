package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupWebSocketAuthUser(t *testing.T, status int) {
	t.Helper()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SecurityFlow{}))
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "tester", Role: common.RoleCommonUser, Status: status}).Error)
	originalDB, originalLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

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
		sid, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: 7, Kind: "session", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		require.NoError(t, err)
		session.Set("security_session", model.SecurityTokenHash(sid))
		session.Set("auth_version", int64(0))
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
	setupWebSocketAuthUser(t, common.UserStatusEnabled)
	recorder := performWebSocketAuthRequest(t, true)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"id":7`)
}

func TestWebSocketUserAuthRejectsAnonymousHandshake(t *testing.T) {
	setupWebSocketAuthUser(t, common.UserStatusEnabled)
	recorder := performWebSocketAuthRequest(t, false)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

// The session cookie still says "enabled" for a month after a ban; the live
// status must win.
func TestWebSocketUserAuthRejectsUserBannedAfterLogin(t *testing.T) {
	setupWebSocketAuthUser(t, common.UserStatusDisabled)
	recorder := performWebSocketAuthRequest(t, true)
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
