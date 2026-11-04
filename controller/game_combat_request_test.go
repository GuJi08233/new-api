package controller

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 普通入门试炼的合法操作记录超过 16 KiB。三个即时战斗入口都应允许提交，并继续拒绝超过专用上限的请求。
func TestWendaoLongCombatReplaySettlesOverHTTP(t *testing.T) {
	require.NoError(t, i18n.Init())
	data, err := os.ReadFile("testdata/wendao_long_replay.json")
	require.NoError(t, err)
	var record struct {
		Seed   uint32      `json:"seed"`
		Setup  doufa.Setup `json:"setup"`
		Inputs string      `json:"inputs"`
	}
	require.NoError(t, common.Unmarshal(data, &record))
	require.Greater(t, len(record.Inputs), 16<<10)
	inputs, err := doufa.DecodeInputs(record.Inputs)
	require.NoError(t, err)
	fight, err := doufa.Replay(record.Seed, record.Setup, inputs)
	require.NoError(t, err)
	require.True(t, fight.Over)
	require.False(t, fight.Won)

	for _, kind := range []string{"duel", "hero", "rift"} {
		t.Run(kind, func(t *testing.T) {
			env := newGameTestEnv(t, sectSettings())
			player := env.player(0, gameTestIpA)
			duelHero(t, player)
			issued := common.GetTimestamp() - 360
			body := map[string]any{"inputs": record.Inputs}
			path := "/api/game/wendao/" + kind + "/clear"
			switch kind {
			case "duel":
				player.ok(http.MethodPost, duelBase+"/arms", map[string]any{"weapons": []string{"talisman", "sword"}, "skill": "quake"})
				player.ok(http.MethodPost, duelBase+"/start", map[string]any{"kind": wendao.DuelTrial, "id": "tianjian"})
				editWendao(t, player, func(save *wendao.Save) {
					active := save.Duels.Active
					require.Equal(t, record.Setup, active.Setup, "this replay uses the actual ordinary entry trial")
					active.Seed, active.Issued = record.Seed, issued
					body["battle_token"] = active.Token
				})
			case "hero":
				player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "visit", "node": "c1_oath"})
				player.ok(http.MethodPost, "/api/game/wendao/hero/action", map[string]any{"action": "fight", "node": "c1_backhill"})
				editWendao(t, player, func(save *wendao.Save) {
					active := save.Hero.Battle.Fight
					active.Seed, active.Setup, active.Issued = record.Seed, record.Setup, issued
					body["seed"], body["issued"], body["battle_token"] = active.Seed, active.Issued, save.Hero.Battle.Token
				})
			case "rift":
				started := player.ok(http.MethodPost, riftBase+"/start", map[string]any{"class": "warrior"})["session"].(map[string]any)
				entered := riftAct(player, started, map[string]any{"action": "enter", "choice": 0})
				body["version"] = editRift(t, player.user.Id, func(run *doufa.Run) {
					run.Room.Seed, run.Room.Setup, run.Room.Issued = record.Seed, record.Setup, issued
				})
				body["session_id"], body["action"] = entered["session_id"], "clear"
				path = riftBase + "/act"
			}
			body["inputs"] = strings.Repeat("a", doufaRequestMaxBytes)
			player.fails(http.MethodPost, path, body)
			body["inputs"] = record.Inputs
			result := player.ok(http.MethodPost, path, body)
			switch kind {
			case "duel":
				assert.Equal(t, false, result["won"])
				assert.EqualValues(t, fight.Tick, result["ticks"])
			case "hero":
				assert.Equal(t, false, result["ended"].(map[string]any)["won"])
				assert.EqualValues(t, fight.Tick, result["ended"].(map[string]any)["ticks"])
			case "rift":
				assert.Equal(t, model.GameSessionStatusFinished, result["session"].(map[string]any)["status"])
			}
		})
	}
}
