package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillStateRestorationRespectsGameAndCenterSwitches(t *testing.T) {
	games := []string{"minesweeper", "sudoku", "memory", "puzzle15", "pairlink", "sokoban", "lightsout", "hanoi"}
	setting := gameSetting(nil)
	enabled := make(map[string]any)
	for _, game := range games {
		enabled[game] = map[string]bool{"enabled": true}
	}
	data, err := common.Marshal(enabled)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, setting))
	env := newGameTestEnv(t, setting)
	player := env.player(0, gameTestIpA)
	for _, game := range games {
		t.Run(game, func(t *testing.T) {
			operation_setting.SetGameSettingForTest(setting)
			path := "/api/game/" + game
			var body any = map[string]int{"difficulty": 1}
			if game == "sokoban" {
				body = nil
			}
			started := player.ok(http.MethodPost, path+"/start", body)
			before := player.ok(http.MethodGet, path+"/state", nil)
			require.NotNil(t, before["session"])
			closed := setting.Clone()
			data, err := common.Marshal(map[string]any{game: map[string]bool{"enabled": false}})
			require.NoError(t, err)
			require.NoError(t, common.Unmarshal(data, closed))
			operation_setting.SetGameSettingForTest(closed)
			player.fails(http.MethodGet, path+"/state", nil)
			closed = setting.Clone()
			closed.Enabled = false
			operation_setting.SetGameSettingForTest(closed)
			player.fails(http.MethodGet, path+"/state", nil)
			operation_setting.SetGameSettingForTest(setting)
			restored := player.ok(http.MethodGet, path+"/state", nil)
			assert.Equal(t, started["session_id"], restored["session"].(map[string]any)["session_id"])
		})
	}
}
