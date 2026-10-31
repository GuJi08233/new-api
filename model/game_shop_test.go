package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 买道具扣币、记流水与改写存档要么一起成功，要么都不发生：余额不够或者存档放不下时，余额、流水和存档都不变。
func TestBuyGameItemIsAtomic(t *testing.T) {
	truncateTables(t)
	const userId = 2101
	seedGameUser(t, userId, 1)
	add := func(data string) (string, error) { return data + "x", nil }

	balance, data, err := BuyGameItem(userId, "sect", coin(1)/2, "sect:item×1", add)
	require.NoError(t, err)
	assert.Equal(t, coin(1)/2, balance)
	assert.Equal(t, "x", data)

	_, _, err = BuyGameItem(userId, "sect", coin(1), "sect:item×2", add)
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	full := errors.New("bag is full")
	_, _, err = BuyGameItem(userId, "sect", coin(1)/4, "sect:item×1", func(string) (string, error) { return "", full })
	assert.ErrorIs(t, err, full)

	assert.Equal(t, coin(1)/2, gameBalance(t, userId))
	save, err := GetGameSave(userId, "sect")
	require.NoError(t, err)
	assert.Equal(t, "x", save)
	var logs []GameCoinLog
	require.NoError(t, DB.Where("user_id = ? AND type = ?", userId, GameCoinLogTypeGameShop).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, -coin(1)/2, logs[0].Amount)
	assert.Equal(t, "sect:item×1", logs[0].Remark)
}

// 对局里用道具时，对局的新状态与存档一起保存；对局已经被另一步改过(版本不对)时存档不动，道具不会白扣。
func TestUpdateGameSessionAndSaveChecksTheVersion(t *testing.T) {
	truncateTables(t)
	const userId = 2102
	seedGameUser(t, userId, 0)
	session, err := CreateGameSession(userId, "dungeon", 7, "a")
	require.NoError(t, err)
	stale := *session
	take := func(data string) (string, error) { return data + "-", nil }

	require.NoError(t, UpdateGameSessionAndSave(session, "b", "", "dungeon", take))
	assert.ErrorIs(t, UpdateGameSessionAndSave(&stale, "c", "", "dungeon", take), ErrGameRoundChanged)

	save, err := GetGameSave(userId, "dungeon")
	require.NoError(t, err)
	assert.Equal(t, "-", save)
	current, err := GetUserGameSession(userId, "dungeon", session.Id)
	require.NoError(t, err)
	assert.Equal(t, "b", current.State)
	assert.Equal(t, session.Version, current.Version)
}
