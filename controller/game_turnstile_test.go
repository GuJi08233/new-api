package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGameTurnstileExemptionsMatchInfoAndFreeStartRoutes(t *testing.T) {
	games := []string{
		"tetris", "2048", "snake", "flappy", "minesweeper", "sudoku", "memory", "puzzle15", "pairlink", "breakout", "sokoban", "lightsout", "hanoi",
		"gomoku", "connect4", "reversi", "xiangqi", "landlord", "wheel",
	}
	setting := gameSetting(nil)
	setting.PlayTurnstileEnabled = true
	enabled := make(map[string]any)
	for _, game := range games {
		enabled[game] = map[string]bool{"enabled": true}
	}
	data, err := common.Marshal(enabled)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, setting))
	env := newGameTestEnv(t, setting)
	player := env.player(100, gameTestIpA)
	previous := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previous })
	requiredMessages := make(map[string]string)
	for _, game := range games {
		t.Run(game, func(t *testing.T) {
			operation_setting.SetGameSettingForTest(setting)
			path := "/api/game/" + game
			info := player.ok(http.MethodGet, path, nil)
			assert.Equal(t, true, info["play_turnstile"].(map[string]any)["required"])
			start := path + "/start"
			var body any
			switch game {
			case "minesweeper", "sudoku", "memory", "puzzle15", "pairlink", "lightsout", "hanoi":
				body = map[string]int{"difficulty": 1}
			case "gomoku", "connect4", "reversi", "xiangqi", "landlord":
				start = path + "/ai/start"
				body = map[string]int{"level": 1}
			case "wheel":
				start = path + "/spin"
				body = map[string]bool{"free": true}
			}
			requiredMessages[game] = player.fails(http.MethodPost, start, body)
			exempt := setting.Clone()
			exempt.PlayTurnstileExemptGames = []string{game}
			operation_setting.SetGameSettingForTest(exempt)
			info = player.ok(http.MethodGet, path, nil)
			assert.Equal(t, false, info["play_turnstile"].(map[string]any)["required"])
			player.ok(http.MethodPost, start, body)
		})
	}
	// 开局使用服务端路由对应的游戏，调用者传另一个已豁免的游戏不能绕过验证。
	exempt := setting.Clone()
	exempt.PlayTurnstileExemptGames = []string{"sudoku"}
	operation_setting.SetGameSettingForTest(exempt)
	before, err := model.GetPlayingGameSession(player.user.Id, "2048")
	require.NoError(t, err)
	require.NotNil(t, before)
	assert.Equal(t, requiredMessages["2048"], player.fails(http.MethodPost, "/api/game/2048/start?game=sudoku", nil))
	assert.Equal(t, requiredMessages["2048"], player.fails(http.MethodPost, "/api/game/2048/start", map[string]string{"game": "sudoku"}))
	after, err := model.GetPlayingGameSession(player.user.Id, "2048")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before.Id, after.Id, "rejected starts do not abandon the saved session")
	// 大转盘付费玩法仍不要求免费验证，也不受免验证列表影响。
	player.ok(http.MethodPost, "/api/game/wheel/spin", map[string]bool{"free": false})
}
