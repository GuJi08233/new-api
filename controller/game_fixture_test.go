package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 测试里玩家的两个来源 IP。同一个 IP 视为同一个人。
const (
	gameTestIpA = "203.0.113.7"
	gameTestIpB = "198.51.100.9"
)

// gameTestUserSeq 给每个测试的玩家分配不同的用户 ID：游戏接口的限流按用户计数、在测试之间共用，同一个 ID 会互相占满次数。
var gameTestUserSeq atomic.Int64

func init() {
	gameTestUserSeq.Store(10000)
}

// gameTestEnv 是一个独立的库、按给定配置发布的游戏中心，以及挂着所有游戏真实路由(含限流中间件)的路由器。
type gameTestEnv struct {
	t      *testing.T
	db     *gorm.DB
	router *gin.Engine
}

// gamePlayer 是一个已登录的玩家，请求都从 ip 发出。
type gamePlayer struct {
	env     *gameTestEnv
	user    model.User
	cookies []*http.Cookie
	ip      string
}

func newGameTestEnv(t *testing.T, setting *operation_setting.GameSetting) *gameTestEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "game.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogs, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	previousSetting := operation_setting.GetGameSetting()
	operation_setting.SetGameSettingForTest(setting)
	t.Cleanup(func() {
		operation_setting.SetGameSettingForTest(previousSetting)
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLogs, previousRedis
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SecurityFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.Log{}, &model.RiskEvent{},
		&model.GameCoinAccount{}, &model.GameCoinLog{}, &model.GameCoinWithdrawDay{}, &model.GameSession{},
		&model.GameRewardDay{}, &model.GameRewardIpDay{}, &model.GameRoom{}, &model.GameJackpotRound{}, &model.GameJackpotEntry{}))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("game-test-cookie-secret"))))
	router.POST("/game-login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		setupLogin(&model.User{Id: id}, c)
	})
	group := router.Group("/api/game", middleware.UserAuth(), middleware.GameCenterEnabled())
	group.GET("/self", GetGameCenterSelf)
	RegisterGameRoutes(group)
	return &gameTestEnv{t: t, db: db, router: router}
}

// gameSetting 返回打开了游戏中心、其余都是默认值的配置，edit 在它上面打开要测的游戏。
func gameSetting(edit func(setting *operation_setting.GameSetting)) *operation_setting.GameSetting {
	setting := operation_setting.DefaultGameSettingForTest()
	setting.Enabled = true
	setting.PlayTurnstileEnabled = false
	if edit != nil {
		edit(setting)
	}
	return setting
}

// player 建一个有 coins 个游戏币、从 ip 访问的玩家并登录。
func (e *gameTestEnv) player(coins int, ip string) *gamePlayer {
	e.t.Helper()
	id := int(gameTestUserSeq.Add(1))
	user := model.User{
		Id:       id,
		Username: fmt.Sprintf("player%d", id),
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		AffCode:  fmt.Sprintf("game-aff-%d", id),
	}
	require.NoError(e.t, e.db.Create(&user).Error)
	if coins > 0 {
		_, err := model.AdjustGameCoins(id, coins, "seed")
		require.NoError(e.t, err)
	}
	player := &gamePlayer{env: e, user: user, ip: ip}
	login := player.send(http.MethodPost, fmt.Sprintf("/game-login/%d", id), nil)
	require.Equal(e.t, http.StatusOK, login.Code, login.Body.String())
	require.Contains(e.t, login.Body.String(), `"success":true`, login.Body.String())
	player.cookies = login.Result().Cookies()
	return player
}

func (p *gamePlayer) send(method string, path string, body any) *httptest.ResponseRecorder {
	p.env.t.Helper()
	data, err := common.Marshal(body)
	require.NoError(p.env.t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("New-Api-User", strconv.Itoa(p.user.Id))
	request.Header.Set("X-Forwarded-For", p.ip)
	for _, c := range p.cookies {
		request.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	p.env.router.ServeHTTP(recorder, request)
	return recorder
}

// call 发一个请求并返回解析后的响应。
func (p *gamePlayer) call(method string, path string, body any) map[string]any {
	p.env.t.Helper()
	recorder := p.send(method, path, body)
	var response map[string]any
	require.NoError(p.env.t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
	return response
}

// ok 发一个请求，要求成功，返回 data。
func (p *gamePlayer) ok(method string, path string, body any) map[string]any {
	p.env.t.Helper()
	response := p.call(method, path, body)
	require.Equal(p.env.t, true, response["success"], "%s %s: %v", method, path, response["message"])
	data, _ := response["data"].(map[string]any)
	return data
}

// fails 发一个请求，要求被拒绝，返回提示。
func (p *gamePlayer) fails(method string, path string, body any) string {
	p.env.t.Helper()
	response := p.call(method, path, body)
	require.Equal(p.env.t, false, response["success"], "%s %s should be rejected", method, path)
	message, _ := response["message"].(string)
	return message
}

func (p *gamePlayer) balance() int {
	p.env.t.Helper()
	account, err := model.GetGameCoinAccount(p.user.Id)
	require.NoError(p.env.t, err)
	return account.Balance
}
