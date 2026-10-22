package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGameSkillTemporaryReadFailureRemainsRetryable(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) { s.Game2048.Enabled = true }))
	player := env.player(0, gameTestIpA)
	started := player.ok(http.MethodPost, "/api/game/2048/start", nil)
	require.NoError(t, env.db.Callback().Query().Before("gorm:query").Register("game-test-read-failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "GameSession" {
			tx.AddError(errors.New("temporary database failure"))
		}
	}))
	body := map[string]any{"session_id": started["session_id"], "moves": ""}
	response := player.send(http.MethodPost, "/api/game/2048/finish", body)
	assert.Equal(t, http.StatusServiceUnavailable, response.Code, "503 instructs the browser to retain its replay")
	require.NoError(t, env.db.Callback().Query().Remove("game-test-read-failure"))
	settled := player.ok(http.MethodPost, "/api/game/2048/finish", body)
	assert.Equal(t, 0.0, settled["score"])
}
