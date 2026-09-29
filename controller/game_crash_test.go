package controller

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/crash"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// crashTestStart 是测试时钟的起点(服务端毫秒时间)。
const crashTestStart int64 = 1_700_000_000_000

// useCrashClock 换上一个停在 crashTestStart、由测试拨动的时钟，返回它的指针；长轮询的等待直接把时钟拨过去。
func useCrashClock(t *testing.T) *int64 {
	t.Helper()
	now := crashTestStart
	previousNow, previousSleep := crashNowMs, crashSleep
	crashNowMs = func() int64 { return now }
	crashSleep = func(_ *gin.Context, d time.Duration) bool {
		now += d.Milliseconds()
		return true
	}
	t.Cleanup(func() { crashNowMs, crashSleep = previousNow, previousSleep })
	return &now
}

// crashAt 让接下来开局的爆点都是 point(百分之一倍)。
func crashAt(t *testing.T, point int) {
	t.Helper()
	previous := newCrashPoint
	newCrashPoint = func(int) (int, error) { return point, nil }
	t.Cleanup(func() { newCrashPoint = previous })
}

func crashSetting(s *operation_setting.GameSetting) {
	s.Crash = operation_setting.WagerRule{Enabled: true, MaxBet: 100, RtpPercent: 96}
}

// 火箭按开局以来的毫秒数计算倍数：爆炸之前兑现按那一刻的倍数派彩 floor(下注 × 倍数 / 100)，到了爆点再兑现就输了；
// 过了爆点或自动兑现的时刻没人兑现时，查看对局就在那里结算。进行中的响应只有开局时刻与服务器时间，结束后才给出爆点；
// 游戏页列出最近几局的爆点。
func TestCrashCashoutAndLazySettlement(t *testing.T) {
	env, _ := newWagerRoundEnv(t, crashSetting)
	player := env.player(100, gameTestIpA)
	now := useCrashClock(t)

	// 1.50× 兑现：7 × 1.5 = 10.5，派彩 10。
	crashAt(t, 250)
	started := player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 7})
	assert.EqualValues(t, 93, started["balance"])
	round := roundOf(t, started)
	assert.EqualValues(t, crashTestStart, round["started_at_ms"])
	assert.EqualValues(t, crashTestStart, round["server_time_ms"])
	assert.EqualValues(t, 0, round["auto_cashout"])
	assert.NotContains(t, round, "crash_point")
	*now = crashTestStart + crash.TimeOf(150)
	flying := roundOf(t, player.ok(http.MethodGet, "/api/game/crash/round", nil))
	assert.Equal(t, model.GameSessionStatusPlaying, flying["status"])
	assert.EqualValues(t, *now, flying["server_time_ms"])
	assert.NotContains(t, flying, "crash_point")
	assert.NotContains(t, player.send(http.MethodGet, "/api/game/crash/round", nil).Body.String(), `"point"`, "爆点不出现在响应里")
	cashed := player.ok(http.MethodPost, "/api/game/crash/cashout", roundRequest(t, started, nil))
	round = roundOf(t, cashed)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 250, round["crash_point"])
	assert.EqualValues(t, 150, round["cashout_multiplier"])
	assert.EqualValues(t, 10, round["win"])
	assert.EqualValues(t, 103, cashed["balance"])

	// 爆炸的那一刻才请求兑现，已经晚了。
	*now = crashTestStart
	started = player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})
	*now = crashTestStart + crash.TimeOf(250)
	lost := player.ok(http.MethodPost, "/api/game/crash/cashout", roundRequest(t, started, nil))
	assert.EqualValues(t, 0, roundOf(t, lost)["win"])
	assert.EqualValues(t, 0, roundOf(t, lost)["cashout_multiplier"])
	assert.EqualValues(t, 93, lost["balance"])

	// 没人兑现：过了爆点查看对局时结算，之后没有进行中的一局，按编号还能看到结果。
	crashAt(t, 300)
	*now = crashTestStart
	player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})
	*now = crashTestStart + crash.TimeOf(300) + 1000
	settled := player.ok(http.MethodGet, "/api/game/crash/round", nil)
	round = roundOf(t, settled)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 300, round["crash_point"])
	assert.EqualValues(t, 0, round["win"])
	assert.EqualValues(t, 83, settled["balance"])
	assert.Nil(t, player.ok(http.MethodGet, "/api/game/crash/round", nil)["round"])
	byId := roundOf(t, player.ok(http.MethodGet, fmt.Sprintf("/api/game/crash/round?session_id=%v", round["session_id"]), nil))
	assert.EqualValues(t, 300, byId["crash_point"])

	// 自动兑现 2.00× 不高于爆点：过了自动兑现的时刻查看对局，按 2.00× 结算。
	*now = crashTestStart
	started = player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10, "auto_cashout": 200})
	assert.EqualValues(t, 200, roundOf(t, started)["auto_cashout"])
	*now = crashTestStart + crash.TimeOf(250)
	auto := player.ok(http.MethodGet, "/api/game/crash/round", nil)
	assert.EqualValues(t, 200, roundOf(t, auto)["cashout_multiplier"])
	assert.EqualValues(t, 20, roundOf(t, auto)["win"])
	assert.EqualValues(t, 93, auto["balance"])
	assert.EqualValues(t, -7, auto["net_today"])
	assert.EqualValues(t, 4, auto["plays_today"])

	info := player.ok(http.MethodGet, "/api/game/crash", nil)
	assert.Equal(t, []any{300.0, 300.0, 250.0, 250.0}, info["recent"], "最近几局的爆点，从新到旧")
	assert.EqualValues(t, 96, info["rtp_percent"])
	assert.EqualValues(t, crash.MaxPoint, info["max_multiplier"])
}

// 长轮询等到火箭自己结束的那一刻再判定：爆点 5.00× 在开局后约 26.8 秒，第一次最多等 25 秒，还在飞；第二次正好等到爆炸，拿到结果。
func TestCrashLongPollWaitsForTheEnd(t *testing.T) {
	env, _ := newWagerRoundEnv(t, crashSetting)
	player := env.player(100, gameTestIpA)
	now := useCrashClock(t)
	crashAt(t, 500)
	player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})

	first := roundOf(t, player.ok(http.MethodGet, "/api/game/crash/round?wait=1", nil))
	assert.Equal(t, model.GameSessionStatusPlaying, first["status"])
	assert.EqualValues(t, crashTestStart+crashPollTimeout.Milliseconds(), *now)
	assert.NotContains(t, first, "crash_point")

	second := player.ok(http.MethodGet, "/api/game/crash/round?wait=1", nil)
	assert.EqualValues(t, crashTestStart+crash.TimeOf(500)+crashSettleGrace.Milliseconds(), *now,
		"等到爆炸之后再等一小会儿：爆炸前一刻发出的兑现先按它收到的时刻结算")
	assert.Equal(t, model.GameSessionStatusFinished, roundOf(t, second)["status"])
	assert.EqualValues(t, 500, roundOf(t, second)["crash_point"])
	assert.EqualValues(t, 90, second["balance"])
}

// 自动兑现不合规则时不能开局；还在飞的一局没结束不能开下一局，已经爆了、只是没人结算的一局在开下一局时结算。
// 兑现要带玩家看到的编号与版本。
func TestCrashRejectsInvalidRequests(t *testing.T) {
	env, _ := newWagerRoundEnv(t, crashSetting)
	player := env.player(100, gameTestIpA)
	now := useCrashClock(t)
	crashAt(t, 250)

	for _, auto := range []int{-1, 100, crash.MaxPoint + 1} {
		assert.Equal(t, gameMessage(i18n.MsgInvalidParams), player.fails(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10, "auto_cashout": auto}), "auto %d", auto)
	}
	started := player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})
	assert.Equal(t, gameMessage(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10}))
	stale := roundRequest(t, started, map[string]any{"version": 1})
	assert.Equal(t, gameMessage(i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/crash/cashout", stale))
	assert.Equal(t, 90, player.balance())

	*now = crashTestStart + crash.TimeOf(250)
	next := player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})
	assert.NotEqual(t, roundOf(t, started)["session_id"], roundOf(t, next)["session_id"])
	assert.EqualValues(t, 80, next["balance"], "上一局爆了，输掉 10 个")
	previous, err := model.GetUserGameSession(player.user.Id, gameCrash, int(roundOf(t, started)["session_id"].(float64)))
	require.NoError(t, err)
	assert.Equal(t, model.GameSessionStatusFinished, previous.Status)
	assert.Zero(t, previous.Reward)
}

// 游戏关闭后不能开新局，但还在飞的一局照样能兑现。
func TestCrashRoundResumableAfterDisabled(t *testing.T) {
	env, setting := newWagerRoundEnv(t, crashSetting)
	player := env.player(100, gameTestIpA)
	now := useCrashClock(t)
	crashAt(t, 250)
	started := player.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})

	setting.Crash.Enabled = false
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10}))
	assert.Equal(t, false, player.ok(http.MethodGet, "/api/game/crash", nil)["enabled"])
	assert.Equal(t, model.GameSessionStatusPlaying, roundOf(t, player.ok(http.MethodGet, "/api/game/crash/round", nil))["status"])
	*now = crashTestStart + crash.TimeOf(200)
	cashed := player.ok(http.MethodPost, "/api/game/crash/cashout", roundRequest(t, started, nil))
	assert.EqualValues(t, 110, cashed["balance"])
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodGet, "/api/game/crash", nil))
}

// 玩家开了一局火箭就离开了：过了最长飞行时间，系统任务按自动兑现或爆点结算，自动兑现的派彩不必等玩家回来；
// 还在最长飞行时间之内的一局不动。
func TestCrashSweepSettlesAbandonedRounds(t *testing.T) {
	env, _ := newWagerRoundEnv(t, crashSetting)
	now := useCrashClock(t)
	auto := env.player(100, gameTestIpA)
	crashed := env.player(100, gameTestIpA)
	flying := env.player(100, gameTestIpA)

	crashAt(t, 500)
	autoRound := roundOf(t, auto.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10, "auto_cashout": 200}))
	crashAt(t, 300)
	crashedRound := roundOf(t, crashed.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10}))
	crashAt(t, crash.MaxPoint)
	flying.ok(http.MethodPost, "/api/game/crash/start", map[string]any{"bet": 10})

	// 前两局开局已经超过最长飞行时间，第三局刚开、还在飞。
	require.NoError(t, env.db.Model(&model.GameSession{}).
		Where("id IN ?", []any{autoRound["session_id"], crashedRound["session_id"]}).
		Update("created_at", gorm.Expr("created_at - ?", crashFlightSeconds+1)).Error)
	*now = crashTestStart + crash.TimeOf(300) + 1

	settled, failed, err := sweepCrashRounds(context.Background(), common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, 2, settled)
	assert.Equal(t, 0, failed)

	assert.Equal(t, 110, auto.balance(), "按 2.00× 自动兑现")
	assert.Nil(t, auto.ok(http.MethodGet, "/api/game/crash/round", nil)["round"])
	assert.Equal(t, 90, crashed.balance())
	assert.EqualValues(t, 1, crashed.ok(http.MethodGet, "/api/game/crash", nil)["plays_today"], "爆了的一局记为输掉的一局")
	assert.Equal(t, 90, flying.balance())
	assert.Equal(t, model.GameSessionStatusPlaying, roundOf(t, flying.ok(http.MethodGet, "/api/game/crash/round", nil))["status"])
}
