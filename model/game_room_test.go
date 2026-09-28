package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestGameRoom(t *testing.T, hostId int, stake int, rakePercent int) *GameRoom {
	t.Helper()
	room := &GameRoom{Game: "gomoku", Stake: stake, Code: "code" + string(rune('a'+hostId%26)), HostId: hostId, HostIp: gameTestIp, RakePercent: rakePercent, MoveSeconds: 60}
	_, err := CreateGameRoom(room)
	require.NoError(t, err)
	return room
}

// 押注对局：开房与加入各扣一份押注，赢家拿走两份减去抽成(按奖池向下取整)，输家的押注计入累计输赢；
// 同一局只结算一次，拿着旧版本的结算不生效。
func TestFinishGameRoomPaysWinnerMinusRake(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2001, 20)
	seedGameUser(t, 2002, 20)
	room := createTestGameRoom(t, 2001, 15, 5)
	assert.Equal(t, 5, gameBalance(t, 2001))

	_, err := CreateGameRoom(&GameRoom{Game: "gomoku", Stake: 1, Code: "second", HostId: 2001, HostIp: gameTestIp})
	assert.ErrorIs(t, err, ErrGameRoomActive, "同一个游戏里只能有一个房间")

	stale := *room
	balance, err := JoinGameRoom(room, 2002, "198.51.100.9", GameRoomStart{State: "{}", ToMove: 0, Deadline: 1})
	require.NoError(t, err)
	assert.Equal(t, 5, balance)
	_, err = JoinGameRoom(&stale, 2003, "198.51.100.10", GameRoomStart{State: "{}"})
	assert.ErrorIs(t, err, ErrGameRoomChanged, "已经有人加入的房间不能再加入")

	finished := *room
	require.NoError(t, FinishGameRoom(&finished, 2002, GameRoomEndWin, "{}", "2026-09-28"))
	assert.Equal(t, 1, finished.Rake, "奖池 30 的 5% 向下取整")
	assert.ErrorIs(t, FinishGameRoom(room, 2001, GameRoomEndWin, "{}", "2026-09-28"), ErrGameRoomChanged)

	assert.Equal(t, 5, gameBalance(t, 2001))
	assert.Equal(t, 34, gameBalance(t, 2002))
	host, err := GetGameCoinAccount(2001)
	require.NoError(t, err)
	guest, err := GetGameCoinAccount(2002)
	require.NoError(t, err)
	assert.Equal(t, -15, host.TotalWagerNet)
	assert.Equal(t, 14, guest.TotalWagerNet)
	net, plays, err := GetGameDayStats(2001, "gomoku", "2026-09-28")
	require.NoError(t, err)
	assert.Equal(t, -15, net)
	assert.Equal(t, 1, plays)
}

// 平局各自退回押注、不抽成；没等到对手就关闭的房间退回房主的押注，这笔押注不算下注过。友谊赛不涉及游戏币。
func TestGameRoomDrawAndCancelRefundStakes(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2101, 10)
	seedGameUser(t, 2102, 10)
	room := createTestGameRoom(t, 2101, 10, 5)
	_, err := JoinGameRoom(room, 2102, "198.51.100.9", GameRoomStart{State: "{}"})
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(room, 0, GameRoomEndDraw, "{}", "2026-09-28"))
	assert.Equal(t, 0, room.Rake)
	assert.Equal(t, 10, gameBalance(t, 2101))
	assert.Equal(t, 10, gameBalance(t, 2102))

	waiting := createTestGameRoom(t, 2101, 4, 5)
	assert.Equal(t, 6, gameBalance(t, 2101))
	require.NoError(t, CancelGameRoom(waiting, GameRoomEndCancel))
	assert.Equal(t, 10, gameBalance(t, 2101))
	account, err := GetGameCoinAccount(2101)
	require.NoError(t, err)
	assert.Equal(t, 10, account.TotalWagered, "只有平局那一局算下注过")

	friendly := createTestGameRoom(t, 2101, 0, 5)
	_, err = JoinGameRoom(friendly, 2102, "198.51.100.9", GameRoomStart{State: "{}"})
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(friendly, 2101, GameRoomEndWin, "{}", "2026-09-28"))
	assert.Equal(t, 10, gameBalance(t, 2101))
	assert.Equal(t, 10, gameBalance(t, 2102))
}

// 余额不够押注时开不了房、加入不了，房间与余额都不变。
func TestGameRoomStakeNeedsBalance(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2201, 3)
	seedGameUser(t, 2202, 3)
	_, err := CreateGameRoom(&GameRoom{Game: "gomoku", Stake: 5, Code: "poor", HostId: 2201, HostIp: gameTestIp})
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	var rooms int64
	require.NoError(t, DB.Model(&GameRoom{}).Count(&rooms).Error)
	assert.Zero(t, rooms)

	room := createTestGameRoom(t, 2201, 3, 5)
	seedGameUser(t, 2203, 2)
	_, err = JoinGameRoom(room, 2203, "198.51.100.9", GameRoomStart{State: "{}"})
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	latest, err := GetGameRoom(room.Id)
	require.NoError(t, err)
	assert.Equal(t, GameRoomStatusWaiting, latest.Status)
	assert.Equal(t, 2, gameBalance(t, 2203))
}

// 夺宝奖池：第二位玩家投入后才开始倒计时；每人每轮的投入不超过上限；到点抽中的奖券决定赢家，赢家拿走奖池减去抽成，
// 输家的投入计入累计输赢；开奖后开始新的一轮。
func TestGameJackpotDrawPaysTicketHolder(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2301, 50)
	seedGameUser(t, 2302, 50)
	rule := GameJackpotEntryRule{MaxBet: 30, RoundSeconds: 60, RakePercent: 10}
	round, balance, err := EnterGameJackpot(2301, 10, rule, gameTestIp)
	require.NoError(t, err)
	assert.Equal(t, 40, balance)
	assert.Zero(t, round.Deadline, "只有一位玩家时不开始倒计时")
	_, _, err = EnterGameJackpot(2301, 21, rule, gameTestIp)
	assert.ErrorIs(t, err, ErrGameJackpotLimit)
	round, _, err = EnterGameJackpot(2302, 25, rule, gameTestOtherIp)
	require.NoError(t, err)
	assert.Equal(t, 35, round.Pot)
	assert.Equal(t, 2, round.Players)
	assert.NotZero(t, round.Deadline)

	drawn, err := DrawGameJackpot(func(int) (int, error) { return 0, nil }, "2026-09-28")
	require.NoError(t, err)
	assert.Nil(t, drawn, "还没到开奖时间")

	require.NoError(t, DB.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Update("deadline", 1).Error)
	// 奖券 0–9 属于第一位玩家，10–34 属于第二位。
	drawn, err = DrawGameJackpot(func(tickets int) (int, error) {
		assert.Equal(t, 35, tickets)
		return 12, nil
	}, "2026-09-28")
	require.NoError(t, err)
	require.NotNil(t, drawn)
	assert.Equal(t, 2302, drawn.WinnerId)
	assert.Equal(t, 25, drawn.WinnerStake)
	assert.Equal(t, 3, drawn.Rake)
	assert.Equal(t, 32, drawn.Payout)
	assert.Equal(t, 40, gameBalance(t, 2301))
	assert.Equal(t, 57, gameBalance(t, 2302))
	loser, err := GetGameCoinAccount(2301)
	require.NoError(t, err)
	assert.Equal(t, -10, loser.TotalWagerNet)

	open, err := GetOpenGameJackpotRound()
	require.NoError(t, err)
	assert.Nil(t, open, "开奖后没有开放中的一轮，下一次投入开新的一轮")
	next, _, err := EnterGameJackpot(2301, 5, rule, gameTestIp)
	require.NoError(t, err)
	assert.NotEqual(t, round.Id, next.Id)
}

// 奖券按玩家编号：玩家按第一次投入的先后排，每位玩家的奖券连在一起，和页面按玩家画的奖池一一对应。A 投 10、B 投 10、
// A 再投 10 时，奖券 0–19 属于 A，20–29 属于 B；抽中 15 号是 A 赢，指针也停在 A 的那一段上。
func TestGameJackpotTicketsFollowPlayerOrder(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2321, 50)
	seedGameUser(t, 2322, 50)
	rule := GameJackpotEntryRule{MaxBet: 30, RoundSeconds: 60, RakePercent: 10}
	_, _, err := EnterGameJackpot(2321, 10, rule, gameTestIp)
	require.NoError(t, err)
	_, _, err = EnterGameJackpot(2322, 10, rule, gameTestOtherIp)
	require.NoError(t, err)
	round, _, err := EnterGameJackpot(2321, 10, rule, gameTestIp)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&GameJackpotRound{}).Where("id = ?", round.Id).Update("deadline", 1).Error)

	drawn, err := DrawGameJackpot(func(int) (int, error) { return 15, nil }, "2026-09-29")
	require.NoError(t, err)
	require.NotNil(t, drawn)
	assert.Equal(t, 2321, drawn.WinnerId)
	assert.Equal(t, 20, drawn.WinnerStake)
}

// 同一 IP 下的账号算同一个人：一个账号投了这一轮，同一 IP 的另一个账号就不能再投，凑不够两位玩家开奖，
// 也越不过每人每轮的上限。换一个 IP 的玩家照常加入。
func TestGameJackpotRejectsSecondAccountOnTheSameIp(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2311, 50)
	seedGameUser(t, 2312, 50)
	seedGameUser(t, 2313, 50)
	rule := GameJackpotEntryRule{MaxBet: 30, RoundSeconds: 60, RakePercent: 10}
	_, _, err := EnterGameJackpot(2311, 30, rule, gameTestIp)
	require.NoError(t, err)

	_, _, err = EnterGameJackpot(2312, 30, rule, gameTestIp)
	require.ErrorIs(t, err, ErrGameJackpotSameIp)
	assert.Equal(t, 50, gameBalance(t, 2312), "被拒绝的投入没有扣币")
	round, err := GetOpenGameJackpotRound()
	require.NoError(t, err)
	assert.Equal(t, 1, round.Players)
	assert.Zero(t, round.Deadline, "一个人凑不够两位玩家")

	_, _, err = EnterGameJackpot(2311, 1, rule, gameTestIp)
	require.ErrorIs(t, err, ErrGameJackpotLimit, "自己的账号照常受每轮上限约束")
	round, _, err = EnterGameJackpot(2313, 10, rule, gameTestOtherIp)
	require.NoError(t, err)
	assert.Equal(t, 2, round.Players)
	assert.NotZero(t, round.Deadline)
}

// 一直只有一位玩家的一轮放久了原样退回，这笔投入不算下注过；有两位玩家的一轮不会被退回。
func TestRefundStaleGameJackpot(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2401, 20)
	rule := GameJackpotEntryRule{MaxBet: 20, RoundSeconds: 60, RakePercent: 5}
	round, _, err := EnterGameJackpot(2401, 8, rule, gameTestIp)
	require.NoError(t, err)

	refunded, err := RefundStaleGameJackpot(round.CreatedAt)
	require.NoError(t, err)
	assert.Nil(t, refunded, "还没放够时间")
	refunded, err = RefundStaleGameJackpot(round.CreatedAt + 1)
	require.NoError(t, err)
	require.NotNil(t, refunded)
	assert.Equal(t, GameJackpotStatusRefunded, refunded.Status)
	account, err := GetGameCoinAccount(2401)
	require.NoError(t, err)
	assert.Equal(t, 20, account.Balance)
	assert.Equal(t, 0, account.TotalWagered)
}
