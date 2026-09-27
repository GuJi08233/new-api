package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWagerRoundEnv 建一个打开了游戏中心、edit 打开了要测的游戏的测试环境，返回环境与它发布的配置(测试可以改它来关掉游戏)。
// 先初始化后端提示语：21 点、挖宝、高低牌、火箭的测试按提示区分请求被拒绝的原因。
func newWagerRoundEnv(t *testing.T, edit func(setting *operation_setting.GameSetting)) (*gameTestEnv, *operation_setting.GameSetting) {
	t.Helper()
	require.NoError(t, i18n.Init())
	setting := gameSetting(edit)
	return newGameTestEnv(t, setting), setting
}

// gameMessage 是接口回应 key 时玩家看到的提示。测试请求不带语言，按英文翻译。
func gameMessage(key string) string {
	return i18n.Translate(i18n.LangEn, key)
}

// roundOf 取出响应里的这一局。
func roundOf(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	round, ok := data["round"].(map[string]any)
	require.True(t, ok, "response has no round: %v", data)
	return round
}

// roundRequest 是继续这一局的请求：带上响应里这一局的编号与版本，再加上 fields。
func roundRequest(t *testing.T, data map[string]any, fields map[string]any) map[string]any {
	t.Helper()
	round := roundOf(t, data)
	body := map[string]any{"session_id": round["session_id"], "version": round["version"]}
	for key, value := range fields {
		body[key] = value
	}
	return body
}

// placeMinesAt 让接下来开局的雷都布在 layout 这些格子上。
func placeMinesAt(t *testing.T, layout ...int) {
	t.Helper()
	previous := placeMines
	placeMines = func(mines int) ([]int, error) {
		require.Equal(t, len(layout), mines, "the test layout has a different number of mines")
		return append([]int(nil), layout...), nil
	}
	t.Cleanup(func() { placeMines = previous })
}

func minesSetting(s *operation_setting.GameSetting) {
	s.Mines = operation_setting.WagerRule{Enabled: true, MaxBet: 100, RtpPercent: 96}
}

// 挖宝一局分几步：开局扣注，每翻开一个安全格可兑现的派彩按 floor(下注 × 96 × C(25,k) / (100 × C(22,k))) 上涨；兑现时派彩，
// 踩雷时输掉下注。进行中的响应不含雷的位置，结束后才给出；结束的一局按编号还能查看。
func TestMinesCashOutAndHitMine(t *testing.T) {
	env, _ := newWagerRoundEnv(t, minesSetting)
	player := env.player(100, gameTestIpA)

	info := player.ok(http.MethodGet, "/api/game/mines", nil)
	assert.EqualValues(t, 96, info["rtp_percent"])
	assert.EqualValues(t, 25, info["grid"])
	assert.EqualValues(t, 100, info["max_bet"])

	placeMinesAt(t, 0, 1, 2)
	started := player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3})
	assert.EqualValues(t, 90, started["balance"])
	round := roundOf(t, started)
	assert.Equal(t, model.GameSessionStatusPlaying, round["status"])
	assert.EqualValues(t, 0, round["version"])
	assert.EqualValues(t, 3, round["mines"])
	assert.Empty(t, round["revealed"])
	assert.EqualValues(t, 0, round["cashout"], "还没翻开安全格，不能兑现")
	assert.EqualValues(t, 10, round["next_cashout"], "10 × 96 × 25 / 2200 = 10.9")
	assert.NotContains(t, round, "mine_cells")
	sessionId := round["session_id"]

	picked := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, started, map[string]any{"index": 5}))
	round = roundOf(t, picked)
	assert.EqualValues(t, 1, round["version"])
	assert.EqualValues(t, []any{5.0}, round["revealed"])
	assert.EqualValues(t, 10, round["cashout"])
	assert.EqualValues(t, 12, round["next_cashout"], "10 × 96 × 300 / 23100 = 12.5")
	assert.NotContains(t, round, "mine_cells")
	picked = player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, picked, map[string]any{"index": 6}))
	assert.EqualValues(t, 12, roundOf(t, picked)["cashout"])

	cashed := player.ok(http.MethodPost, "/api/game/mines/cashout", roundRequest(t, picked, nil))
	round = roundOf(t, cashed)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 12, round["win"])
	assert.EqualValues(t, 2, round["net"])
	assert.EqualValues(t, []any{0.0, 1.0, 2.0}, round["mine_cells"])
	assert.NotContains(t, round, "exploded")
	assert.EqualValues(t, 102, cashed["balance"])
	assert.EqualValues(t, 2, cashed["net_today"])
	assert.EqualValues(t, 1, cashed["plays_today"])

	assert.Nil(t, player.ok(http.MethodGet, "/api/game/mines/round", nil)["round"], "没有进行中的一局")
	finished := roundOf(t, player.ok(http.MethodGet, fmt.Sprintf("/api/game/mines/round?session_id=%v", sessionId), nil))
	assert.EqualValues(t, 12, finished["win"], "按编号查看结束的一局")
	assert.EqualValues(t, []any{0.0, 1.0, 2.0}, finished["mine_cells"])

	started = player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3})
	picked = player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, started, map[string]any{"index": 12}))
	lost := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, picked, map[string]any{"index": 1}))
	round = roundOf(t, lost)
	assert.Equal(t, model.GameSessionStatusFinished, round["status"])
	assert.EqualValues(t, 0, round["win"])
	assert.EqualValues(t, -10, round["net"])
	assert.EqualValues(t, 1, round["exploded"])
	assert.EqualValues(t, []any{12.0}, round["revealed"])
	assert.EqualValues(t, []any{0.0, 1.0, 2.0}, round["mine_cells"])
	assert.EqualValues(t, 92, lost["balance"])
	assert.EqualValues(t, -8, lost["net_today"])
	assert.EqualValues(t, 2, lost["plays_today"])

	account, err := model.GetGameCoinAccount(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, 20, account.TotalWagered)
	assert.Equal(t, -8, account.TotalWagerNet)
}

// 翻开所有安全格、或派彩到了下注的 1000 倍时自动兑现；达到下注 10 倍的派彩记大奖审计日志。
func TestMinesAutoCashout(t *testing.T) {
	env, _ := newWagerRoundEnv(t, minesSetting)
	player := env.player(100, gameTestIpA)

	// 24 个雷只留第 7 格：翻开它就翻完了，24 倍。
	allButSeven := make([]int, 0, 24)
	for cell := 0; cell < 25; cell++ {
		if cell != 7 {
			allButSeven = append(allButSeven, cell)
		}
	}
	placeMinesAt(t, allButSeven...)
	started := player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 2, "mines": 24})
	cleared := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, started, map[string]any{"index": 7}))
	assert.Equal(t, model.GameSessionStatusFinished, roundOf(t, cleared)["status"])
	assert.EqualValues(t, 48, roundOf(t, cleared)["win"])
	assert.EqualValues(t, 146, cleared["balance"])

	// 20 个雷(第 0 到 19 格)：翻开 3 格可兑现 1104，第 4 格按公式 2428.8 倍，封顶 1000 倍自动兑现。
	placeMinesAt(t, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19)
	data := player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 5, "mines": 20})
	for _, cell := range []int{20, 21, 22} {
		data = player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, data, map[string]any{"index": cell}))
	}
	assert.EqualValues(t, 1104, roundOf(t, data)["cashout"])
	assert.EqualValues(t, 5000, roundOf(t, data)["next_cashout"])
	capped := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, data, map[string]any{"index": 23}))
	assert.Equal(t, model.GameSessionStatusFinished, roundOf(t, capped)["status"])
	assert.EqualValues(t, 5000, roundOf(t, capped)["win"])
	assert.EqualValues(t, 146-5+5000, capped["balance"])

	var bigWinLogs int64
	require.NoError(t, env.db.Model(&model.Log{}).Where("user_id = ? AND type = ? AND content LIKE ?", player.user.Id, model.LogTypeSystem, "游戏大奖%").Count(&bigWinLogs).Error)
	assert.EqualValues(t, 2, bigWinLogs)
}

// 雷数、下注不合规则时不能开局；同一时间只能有一局。继续一局的请求要带玩家看到的编号与版本：版本过时、编号不对都拒绝，
// 不会改动这一局；翻开过的格子、越界的格子不能翻，没翻开安全格不能兑现。别的玩家碰不到这一局。
func TestMinesRejectsInvalidRequests(t *testing.T) {
	env, _ := newWagerRoundEnv(t, minesSetting)
	player := env.player(100, gameTestIpA)
	other := env.player(100, gameTestIpB)

	for _, body := range []map[string]any{{"bet": 10, "mines": 0}, {"bet": 10, "mines": 25}} {
		assert.Equal(t, gameMessage(i18n.MsgInvalidParams), player.fails(http.MethodPost, "/api/game/mines/start", body))
	}
	for _, body := range []map[string]any{{"bet": 0, "mines": 3}, {"bet": 101, "mines": 3}} {
		player.fails(http.MethodPost, "/api/game/mines/start", body)
	}
	assert.Equal(t, 100, player.balance())

	placeMinesAt(t, 0, 1, 2)
	started := player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3})
	assert.Equal(t, gameMessage(i18n.MsgGameRoundOpen), player.fails(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3}))
	assert.Equal(t, 90, player.balance(), "第二局没有开成，只扣了一次")
	assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/mines/cashout", roundRequest(t, started, nil)))

	picked := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, started, map[string]any{"index": 5}))
	assert.Equal(t, gameMessage(i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/mines/pick", roundRequest(t, started, map[string]any{"index": 6})), "版本过时")
	wrongSession := roundRequest(t, picked, map[string]any{"index": 6})
	wrongSession["session_id"] = roundOf(t, picked)["session_id"].(float64) + 1
	assert.Equal(t, gameMessage(i18n.MsgGameRoundChanged), player.fails(http.MethodPost, "/api/game/mines/pick", wrongSession))
	for _, index := range []int{5, 25, -1} {
		assert.Equal(t, gameMessage(i18n.MsgGameMoveInvalid), player.fails(http.MethodPost, "/api/game/mines/pick", roundRequest(t, picked, map[string]any{"index": index})), "index %d", index)
	}
	current := roundOf(t, player.ok(http.MethodGet, "/api/game/mines/round", nil))
	assert.EqualValues(t, 1, current["version"], "被拒绝的请求没有改动这一局")
	assert.EqualValues(t, []any{5.0}, current["revealed"])
	assert.NotContains(t, current, "mine_cells")
	assert.NotContains(t, player.send(http.MethodGet, "/api/game/mines/round", nil).Body.String(), "layout", "雷的位置不出现在响应里")

	assert.Equal(t, gameMessage(i18n.MsgGameRoundNotFound), other.fails(http.MethodPost, "/api/game/mines/cashout", roundRequest(t, picked, nil)))
	assert.Nil(t, other.ok(http.MethodGet, fmt.Sprintf("/api/game/mines/round?session_id=%v", current["session_id"]), nil)["round"], "看不到别人的对局")
}

// 游戏关闭后不能开新局，但押着游戏币的一局还能回来玩完：游戏页、查看对局、翻格子、兑现照常；玩完之后游戏页也关闭了。
func TestMinesRoundResumableAfterDisabled(t *testing.T) {
	env, setting := newWagerRoundEnv(t, minesSetting)
	player := env.player(100, gameTestIpA)
	placeMinesAt(t, 0, 1, 2)
	player.ok(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3})

	setting.Mines.Enabled = false
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodPost, "/api/game/mines/start", map[string]any{"bet": 10, "mines": 3}))
	info := player.ok(http.MethodGet, "/api/game/mines", nil)
	assert.Equal(t, false, info["enabled"])
	current := player.ok(http.MethodGet, "/api/game/mines/round", nil)
	picked := player.ok(http.MethodPost, "/api/game/mines/pick", roundRequest(t, current, map[string]any{"index": 9}))
	cashed := player.ok(http.MethodPost, "/api/game/mines/cashout", roundRequest(t, picked, nil))
	assert.EqualValues(t, 100, cashed["balance"], "翻开一格兑现 10 个")
	assert.Equal(t, gameMessage(i18n.MsgGamePlayDisabled), player.fails(http.MethodGet, "/api/game/mines", nil))
}
