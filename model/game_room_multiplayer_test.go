package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 创建已经坐满的测试房间，所有成员从相同余额开始，结算前已各自扣下押注。
func createTestMultiplayerRoom(t *testing.T, count int, stake GameCoins, rakePercent int) *GameRoom {
	t.Helper()
	truncateTables(t)
	for i := 0; i < count; i++ {
		seedGameUser(t, 2601+i, 100)
	}
	room := &GameRoom{Game: "cards", HostId: 2601, Stake: stake, Escrow: stake, Seats: count, RakePercent: rakePercent}
	_, err := CreateGameRoom(room, gameTestIp)
	require.NoError(t, err)
	for i := 1; i < count; i++ {
		_, err = JoinGameRoom(room, 2601+i, fmt.Sprintf("192.0.2.%d", i), startTestGameRoom)
		require.NoError(t, err)
	}
	require.Equal(t, GameRoomStatusPlaying, room.Status)
	return room
}

func TestGameRoomTeamResultsDoNotDependOnSettledNet(t *testing.T) {
	for _, test := range []struct {
		name      string
		stake     GameCoins
		rake      int
		result    GameRoomResult
		wantNets  []GameCoins
		wantRake  GameCoins
		wantSeats []string
	}{
		{
			name:     "friendly teams",
			result:   GameRoomResult{Nets: coins(0, 0, 0), WinnerSeat: 1, Winners: []int{1, 2}},
			wantNets: coins(0, 0, 0), wantSeats: []string{"loss", "win", "win"},
		},
		{
			name:  "zero gain teammate",
			stake: coin(1), rake: 10,
			result:   GameRoomResult{Nets: coins(-1, 1, 0), WinnerSeat: 1, Winners: []int{1, 2}},
			wantNets: []GameCoins{-coin(1), coinText("0.9"), 0}, wantRake: coinText("0.1"), wantSeats: []string{"loss", "win", "win"},
		},
		{
			name:  "team rake applies to each net gain",
			stake: coin(11), rake: 20,
			result:   GameRoomResult{Nets: coins(-11, 6, 5), WinnerSeat: 1, Winners: []int{1, 2}},
			wantNets: []GameCoins{-coin(11), coinText("4.8"), coin(4)}, wantRake: coinText("2.2"), wantSeats: []string{"loss", "win", "win"},
		},
		{
			name:  "two player pool rake can make winner net negative",
			stake: coin(5), rake: 100,
			result:   GameRoomResult{Nets: coins(-5, 5), WinnerSeat: 1},
			wantNets: coins(-5, -5), wantRake: coin(10), wantSeats: []string{"loss", "win"},
		},
		{
			name:     "friendly draw",
			result:   GameRoomResult{Nets: coins(0, 0, 0), WinnerSeat: -1},
			wantNets: coins(0, 0, 0), wantSeats: []string{"draw", "draw", "draw"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			room := createTestMultiplayerRoom(t, len(test.result.Nets), test.stake, test.rake)
			stale := *room
			reason := GameRoomEndWin
			if test.result.WinnerSeat < 0 {
				reason = GameRoomEndDraw
			}
			require.NoError(t, FinishGameRoom(room, test.result, reason, "finished", "2026-10-10"))
			assert.Equal(t, test.wantRake, room.Rake)
			seats, err := GetGameRoomSeats([]int{room.Id})
			require.NoError(t, err)
			require.Len(t, seats[room.Id], len(test.wantSeats))
			total := room.Rake
			for i, seat := range seats[room.Id] {
				assert.Equal(t, test.wantSeats[i], seat.Result)
				assert.Equal(t, test.wantNets[i], seat.Net)
				balance := gameBalance(t, seat.UserId)
				assert.Equal(t, coin(100)+test.wantNets[i], balance)
				total += balance
				net, plays, err := GetGameDayStats(seat.UserId, room.Game, "2026-10-10")
				require.NoError(t, err)
				assert.Equal(t, test.wantNets[i], net)
				assert.Equal(t, 1, plays, "零押注与零收益成员也计入一次对局")
				if test.stake > 0 && test.wantSeats[i] == "win" {
					var logs []GameCoinLog
					require.NoError(t, DB.Where("user_id = ? AND ref_id = ? AND remark = ?", seat.UserId, gameRoomRef(room.Id), room.Game+":win").Find(&logs).Error)
					if test.stake+test.wantNets[i] > 0 {
						require.Len(t, logs, 1, "零收益赢家的流水仍然记为获胜")
					}
				}
			}
			assert.Equal(t, coin(len(test.wantSeats)*100), total, "玩家余额与抽成守恒")
			assert.ErrorIs(t, FinishGameRoom(&stale, test.result, GameRoomEndWin, "duplicate", "2026-10-10"), ErrGameRoomChanged)
			for i, seat := range seats[room.Id] {
				assert.Equal(t, coin(100)+test.wantNets[i], gameBalance(t, seat.UserId), "重复结算不重复派彩")
				_, plays, err := GetGameDayStats(seat.UserId, room.Game, "2026-10-10")
				require.NoError(t, err)
				assert.Equal(t, 1, plays, "重复结算不增加对局计数")
			}
		})
	}
}

func TestGameRoomRejectsInconsistentTeamResultsWithoutChangingBalances(t *testing.T) {
	room := createTestMultiplayerRoom(t, 3, coin(5), 0)
	for _, test := range []struct {
		name   string
		result GameRoomResult
	}{
		{"winner outside room", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{1, 3}}},
		{"negative winner seat", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{-1, 1, 2}}},
		{"duplicate winner", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{1, 1, 2}}},
		{"missing positive teammate", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{1}}},
		{"negative gain winner", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{0, 1, 2}}},
		{"representative not a winner", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 0, Winners: []int{1, 2}}},
		{"representative not highest gain", GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 2, Winners: []int{1, 2}}},
		{"draw with team winners", GameRoomResult{Nets: coins(0, 0, 0), WinnerSeat: -1, Winners: []int{1, 2}}},
		{"paid team without gain", GameRoomResult{Nets: coins(0, 0, 0), WinnerSeat: 1, Winners: []int{1, 2}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Error(t, FinishGameRoom(room, test.result, GameRoomEndWin, "invalid", "2026-10-10"))
			latest, err := GetGameRoom(room.Id)
			require.NoError(t, err)
			assert.Equal(t, GameRoomStatusPlaying, latest.Status)
			assert.Equal(t, room.Version, latest.Version)
			for i := 0; i < 3; i++ {
				assert.Equal(t, coin(95), gameBalance(t, 2601+i))
			}
		})
	}
}

func TestMultiplayerRoomSettlementOverflowRollsBackWholeTeam(t *testing.T) {
	room := createTestMultiplayerRoom(t, 3, coin(5), 10)
	// 最后一位获胜玩家已经接近余额上限，前面的输家与赢家写账也必须一起回滚。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", 2603).Update("balance", common.MaxQuota-5).Error)
	result := GameRoomResult{Nets: coins(-5, 3, 2), WinnerSeat: 1, Winners: []int{1, 2}}
	assert.ErrorIs(t, FinishGameRoom(room, result, GameRoomEndWin, "finished", "2026-10-10"), ErrGameCoinAmountInvalid)
	latest, err := GetGameRoom(room.Id)
	require.NoError(t, err)
	assert.Equal(t, GameRoomStatusPlaying, latest.Status)
	assert.Equal(t, room.Version, latest.Version)
	assert.Zero(t, latest.Rake)
	assert.Equal(t, coin(95), gameBalance(t, 2601))
	assert.Equal(t, coin(95), gameBalance(t, 2602))
	assert.Equal(t, GameCoins(common.MaxQuota-5), gameBalance(t, 2603))
	seats, err := GetGameRoomSeats([]int{room.Id})
	require.NoError(t, err)
	for _, seat := range seats[room.Id] {
		assert.Zero(t, seat.Net)
		assert.Empty(t, seat.Result)
		account, err := GetGameCoinAccount(seat.UserId)
		require.NoError(t, err)
		assert.Zero(t, account.TotalWagerNet)
		net, plays, err := GetGameDayStats(seat.UserId, room.Game, "2026-10-10")
		require.NoError(t, err)
		assert.Zero(t, net)
		assert.Zero(t, plays)
	}
	var logs []GameCoinLog
	require.NoError(t, DB.Where("ref_id = ?", gameRoomRef(room.Id)).Find(&logs).Error)
	require.Len(t, logs, 3, "失败结算不得留下派彩流水")
	for _, log := range logs {
		assert.Equal(t, room.Game+":stake", log.Remark)
	}
}
