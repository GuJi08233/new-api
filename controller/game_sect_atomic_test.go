package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 会话创建失败时带队丹药和自然结算都回滚；成功开局只扣一次，有效旧局不会被重开或再次扣丹。
func TestSectAtomicStartRollsBackAndKeepsTheExistingRound(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	player := env.player(0, gameTestIpA)
	foundTestSect(t, player)
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	first := save.Sect.Disciples[0].Id
	before, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	failCreate := true
	const callbackName = "sect-start-create-failure"
	require.NoError(t, env.db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if failCreate && tx.Statement.Table == "game_sessions" {
			tx.AddError(errors.New("session storage failure"))
		}
	}))
	t.Cleanup(func() { require.NoError(t, env.db.Callback().Create().Remove(callbackName)) })
	path := "/api/game/wendao/sect/tournament/start"
	body := map[string]any{"team": []int{first}}
	player.fails(http.MethodPost, path, body)
	afterFailure, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	assert.Equal(t, before, afterFailure, "the rejected creation cannot consume pills or persist its preliminary Tick")
	playing, err := model.GetPlayingGameSession(player.user.Id, sectTournamentGame)
	require.NoError(t, err)
	assert.Nil(t, playing)

	failCreate = false
	started := player.ok(http.MethodPost, path, body)
	assert.Equal(t, 2.0, started["tournament"].(map[string]any)["pills"])
	afterStart, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Zero(t, afterStart.Sect.Pills[wendao.PillHeal])
	player.fails(http.MethodPost, path, body)
	stillPlaying, err := model.GetPlayingGameSession(player.user.Id, sectTournamentGame)
	require.NoError(t, err)
	require.NotNil(t, stillPlaying)
	assert.EqualValues(t, started["session_id"], stillPlaying.Id)
	stillSaved, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Zero(t, stillSaved.Sect.Pills[wendao.PillHeal])
}

// 原子大比开局仍按自己的游戏策略要求人机验证；缺令牌不扣丹、不建会话，明确豁免后才可进入。
func TestSectAtomicStartPreservesTurnstilePolicy(t *testing.T) {
	require.NoError(t, i18n.Init())
	setting := sectSettings()
	setting.PlayTurnstileEnabled = true
	env := newGameTestEnv(t, setting)
	player := env.player(0, gameTestIpA)
	foundTestSect(t, player)
	save, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	first := save.Sect.Disciples[0].Id
	previous := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = previous })
	path := "/api/game/wendao/sect/tournament/start"
	body := map[string]any{"team": []int{first}}
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameTurnstileRequired), player.fails(http.MethodPost, path, body))
	playing, err := model.GetPlayingGameSession(player.user.Id, sectTournamentGame)
	require.NoError(t, err)
	assert.Nil(t, playing)
	stillSaved, err := loadWendaoSave(player.user.Id)
	require.NoError(t, err)
	assert.Equal(t, save.Sect.Pills[wendao.PillHeal], stillSaved.Sect.Pills[wendao.PillHeal])
	exempt := setting.Clone()
	exempt.PlayTurnstileExemptGames = []string{sectTournamentGame}
	operation_setting.SetGameSettingForTest(exempt)
	player.ok(http.MethodPost, path, body)
}
