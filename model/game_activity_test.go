package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 存档活动先开始时对局不得开启；对局先开始时活动不得保存。被拒绝的开局保留原对局，活动结束后可再次开局。
func TestGameActivitySaveAndSessionAreExclusive(t *testing.T) {
	truncateTables(t)
	const userId = 2111
	seedGameUser(t, userId, 0)
	activeSince := common.GetTimestamp() - 3600
	_, err := UpdateGameSaveWithoutSession(userId, "journey", "rift", activeSince, func(string) (string, error) {
		return "travelling", nil
	})
	require.NoError(t, err)
	busy := errors.New("the hero is travelling")
	prepare := func(data string) (string, error) {
		if data == "travelling" {
			return "", busy
		}
		return "room:" + data, nil
	}
	_, err = CreateGameSessionWithSave(userId, "rift", 7, activeSince, "journey", prepare)
	require.ErrorIs(t, err, busy)
	playing, err := GetPlayingGameSession(userId, "rift")
	require.NoError(t, err)
	assert.Nil(t, playing)

	_, err = UpdateGameSave(userId, "journey", func(string) (string, error) { return "returned", nil })
	require.NoError(t, err)
	session, err := CreateGameSessionWithSave(userId, "rift", 9, activeSince, "journey", prepare)
	require.NoError(t, err)
	assert.Equal(t, "room:returned", session.State, "prepare reads the latest locked save")
	assert.Equal(t, int64(9), session.Seed)

	_, err = CreateGameSessionWithSave(userId, "rift", 11, activeSince, "journey", prepare)
	assert.ErrorIs(t, err, ErrGameRoundOpen)
	_, err = UpdateGameSaveWithoutSession(userId, "journey", "rift", activeSince, func(string) (string, error) {
		return "travelling", nil
	})
	assert.ErrorIs(t, err, ErrGameRoundOpen)
	data, err := GetGameSave(userId, "journey")
	require.NoError(t, err)
	assert.Equal(t, "returned", data)
	playing, err = GetPlayingGameSession(userId, "rift")
	require.NoError(t, err)
	require.NotNil(t, playing)
	assert.Equal(t, session.Id, playing.Id)
	assert.Equal(t, "room:returned", playing.State)

	require.NoError(t, DB.Model(session).Update("status", GameSessionStatusFinished).Error)
	data, err = UpdateGameSaveWithoutSession(userId, "journey", "rift", activeSince, func(string) (string, error) {
		return "travelling", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "travelling", data)
}

// 已完成请求的收据优先于后来开启的对局；计算新存档失败时，原存档、版本和对局均保留。
func TestGameActivityRetryAndFailuresPreserveTheSave(t *testing.T) {
	truncateTables(t)
	const userId = 2112
	seedGameUser(t, userId, 0)
	_, err := UpdateGameSave(userId, "journey", func(string) (string, error) { return "receipt", nil })
	require.NoError(t, err)
	activeSince := common.GetTimestamp() - 3600
	session, err := CreateGameSessionWithSave(userId, "rift", 1, activeSince, "journey", func(data string) (string, error) {
		return data, nil
	})
	require.NoError(t, err)
	alreadyDone := errors.New("the receipt already exists")
	_, err = UpdateGameSaveWithoutSession(userId, "journey", "rift", activeSince, func(data string) (string, error) {
		assert.Equal(t, "receipt", data)
		return "", alreadyDone
	})
	assert.ErrorIs(t, err, alreadyDone)
	var save GameSave
	require.NoError(t, DB.Where("user_id = ? AND game = ?", userId, "journey").First(&save).Error)
	assert.Equal(t, "receipt", save.Data)
	assert.Equal(t, 1, save.Version, "opening or rejecting an activity does not change the save version")
	current, err := GetUserGameSession(userId, "rift", session.Id)
	require.NoError(t, err)
	assert.Equal(t, GameSessionStatusPlaying, current.Status)

	for _, kind := range []string{"session", "save"} {
		t.Run(kind, func(t *testing.T) {
			refused := errors.New("not available")
			if kind == "session" {
				_, err = CreateGameSessionWithSave(userId+1, "rift", 1, activeSince, "journey", func(string) (string, error) {
					return "", refused
				})
			} else {
				_, err = UpdateGameSaveWithoutSession(userId+1, "journey", "rift", activeSince, func(string) (string, error) {
					return "", refused
				})
			}
			require.ErrorIs(t, err, refused)
			var count int64
			require.NoError(t, DB.Model(&GameSave{}).Where("user_id = ?", userId+1).Count(&count).Error)
			assert.Zero(t, count, "a failed first activity rolls back its placeholder save")
			require.NoError(t, DB.Model(&GameSession{}).Where("user_id = ?", userId+1).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

// 过期、已结束、其他玩家或其他游戏的对局不拦活动；新开局保留过期记录。
func TestGameActivityOnlyBlocksThePlayersLiveGame(t *testing.T) {
	truncateTables(t)
	const userId = 2114
	seedGameUser(t, userId, 0)
	activeSince := common.GetTimestamp() - 3600
	old := []GameSession{
		{UserId: userId, Game: "rift", Status: GameSessionStatusPlaying, CreatedAt: activeSince - 1},
		{UserId: userId, Game: "rift", Status: GameSessionStatusFinished, CreatedAt: activeSince},
		{UserId: userId + 1, Game: "rift", Status: GameSessionStatusPlaying, CreatedAt: activeSince},
		{UserId: userId, Game: "other", Status: GameSessionStatusPlaying, CreatedAt: activeSince},
	}
	require.NoError(t, DB.Create(&old).Error)
	data, err := UpdateGameSaveWithoutSession(userId, "journey", "rift", activeSince, func(string) (string, error) { return "ready", nil })
	require.NoError(t, err)
	assert.Equal(t, "ready", data)
	started, err := CreateGameSessionWithSave(userId, "rift", 3, activeSince, "journey", func(data string) (string, error) { return data, nil })
	require.NoError(t, err)
	assert.Equal(t, "ready", started.State)
	var count int64
	require.NoError(t, DB.Model(&GameSession{}).Count(&count).Error)
	assert.EqualValues(t, len(old)+1, count)
	_, err = UpdateGameSaveWithoutSession(userId, "journey", "rift", started.CreatedAt, func(string) (string, error) { return "blocked", nil })
	assert.ErrorIs(t, err, ErrGameRoundOpen, "the expiration boundary includes its exact timestamp")
}
