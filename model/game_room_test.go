package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestGameRoom(t *testing.T, hostId int, stake int, rakePercent int) *GameRoom {
	t.Helper()
	room := &GameRoom{Game: "gomoku", Stake: stake, Escrow: stake, Seats: 2, Code: "code" + string(rune('a'+hostId%26)), HostId: hostId, RakePercent: rakePercent, MoveSeconds: 60}
	_, err := CreateGameRoom(room, gameTestIp)
	require.NoError(t, err)
	return room
}

func startTestGameRoom([]GameRoomSeat) (GameRoomStart, error) {
	return GameRoomStart{State: "{}", ToMove: 0, Deadline: 1}, nil
}

// 押注对局：开房与加入各扣一份押注，赢家拿走两份减去抽成(按奖池向下取整)，输家的押注计入累计输赢；
// 同一局只结算一次，拿着旧版本的结算不生效。
func TestFinishGameRoomPaysWinnerMinusRake(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2001, 20)
	seedGameUser(t, 2002, 20)
	room := createTestGameRoom(t, 2001, 15, 5)
	assert.Equal(t, 5, gameBalance(t, 2001))

	_, err := CreateGameRoom(&GameRoom{Game: "gomoku", Stake: 1, Escrow: 1, Seats: 2, Code: "second", HostId: 2001}, gameTestIp)
	assert.ErrorIs(t, err, ErrGameRoomActive, "同一个游戏里只能有一个房间")

	stale := *room
	balance, err := JoinGameRoom(room, 2002, "198.51.100.9", startTestGameRoom)
	require.NoError(t, err)
	assert.Equal(t, 5, balance)
	_, err = JoinGameRoom(&stale, 2003, "198.51.100.10", startTestGameRoom)
	assert.ErrorIs(t, err, ErrGameRoomChanged, "已经有人加入的房间不能再加入")

	finished := *room
	require.NoError(t, FinishGameRoom(&finished, GameRoomResult{Nets: []int{-15, 15}, WinnerSeat: 1}, GameRoomEndWin, "{}", "2026-09-28"))
	assert.Equal(t, 1, finished.Rake, "奖池 30 的 5% 向下取整")
	assert.ErrorIs(t, FinishGameRoom(room, GameRoomResult{Nets: []int{15, -15}, WinnerSeat: 0}, GameRoomEndWin, "{}", "2026-09-28"), ErrGameRoomChanged)

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
	_, err := JoinGameRoom(room, 2102, "198.51.100.9", startTestGameRoom)
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(room, GameRoomResult{Nets: []int{0, 0}, WinnerSeat: -1}, GameRoomEndDraw, "{}", "2026-09-28"))
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
	_, err = JoinGameRoom(friendly, 2102, "198.51.100.9", startTestGameRoom)
	require.NoError(t, err)
	require.NoError(t, FinishGameRoom(friendly, GameRoomResult{Nets: []int{0, 0}, WinnerSeat: 0}, GameRoomEndWin, "{}", "2026-09-28"))
	assert.Equal(t, 2101, friendly.WinnerId)
	assert.Equal(t, 10, gameBalance(t, 2101))
	assert.Equal(t, 10, gameBalance(t, 2102))
}

// 余额不够押注时开不了房、加入不了，房间与余额都不变。
func TestGameRoomStakeNeedsBalance(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2201, 3)
	seedGameUser(t, 2202, 3)
	_, err := CreateGameRoom(&GameRoom{Game: "gomoku", Stake: 5, Escrow: 5, Seats: 2, Code: "poor", HostId: 2201}, gameTestIp)
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	var rooms int64
	require.NoError(t, DB.Model(&GameRoom{}).Count(&rooms).Error)
	assert.Zero(t, rooms)

	room := createTestGameRoom(t, 2201, 3, 5)
	seedGameUser(t, 2203, 2)
	_, err = JoinGameRoom(room, 2203, "198.51.100.9", startTestGameRoom)
	assert.ErrorIs(t, err, ErrGameCoinInsufficient)
	latest, err := GetGameRoom(room.Id)
	require.NoError(t, err)
	assert.Equal(t, GameRoomStatusWaiting, latest.Status)
	assert.Equal(t, 2, gameBalance(t, 2203))
}

func TestMultiplayerRoomLeaveReusesSeatAndHostRefundsEveryone(t *testing.T) {
	truncateTables(t)
	for _, id := range []int{2501, 2502, 2503} {
		seedGameUser(t, id, 100)
	}
	room := &GameRoom{Game: "cards", HostId: 2501, Stake: 5, Escrow: 20, Seats: 4}
	_, err := CreateGameRoom(room, gameTestIp)
	require.NoError(t, err)
	_, err = JoinGameRoom(room, 2502, gameTestOtherIp, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, room.Players)
	assert.Equal(t, GameRoomStatusWaiting, room.Status)
	_, err = JoinGameRoom(room, 2503, gameTestOtherIp, nil)
	assert.ErrorIs(t, err, ErrGameRoomSameIp, "所有已入座玩家的 IP 都参与检测")
	assert.Equal(t, 100, gameBalance(t, 2503))

	require.NoError(t, LeaveGameRoom(room, 2502))
	assert.Equal(t, 1, room.Players)
	assert.Equal(t, 100, gameBalance(t, 2502))
	assert.ErrorIs(t, LeaveGameRoom(room, 2502), ErrGameRoomNotFound)
	_, err = JoinGameRoom(room, 2503, gameTestOtherIp, nil)
	require.NoError(t, err)
	seats, err := GetGameRoomSeats([]int{room.Id})
	require.NoError(t, err)
	require.Len(t, seats[room.Id], 2)
	assert.Equal(t, 1, seats[room.Id][1].Seat)
	assert.Equal(t, 2503, seats[room.Id][1].UserId)
	active, err := GetUserActiveGameRooms(2502)
	require.NoError(t, err)
	assert.Empty(t, active)

	require.NoError(t, LeaveGameRoom(room, 2501))
	assert.Equal(t, GameRoomStatusCancelled, room.Status)
	assert.ErrorIs(t, CancelGameRoom(room, GameRoomEndCancel), ErrGameRoomChanged)
	for _, id := range []int{2501, 2502, 2503} {
		account, err := GetGameCoinAccount(id)
		require.NoError(t, err)
		assert.Equal(t, 100, account.Balance)
		assert.Zero(t, account.TotalWagered)
	}
}

func TestMultiplayerRoomStartAndSettlementAreAtomic(t *testing.T) {
	truncateTables(t)
	for _, id := range []int{2511, 2512, 2513, 2514} {
		seedGameUser(t, id, 100)
	}
	room := &GameRoom{Game: "cards", HostId: 2511, Stake: 5, Escrow: 20, Seats: 4, RakePercent: 10}
	_, err := CreateGameRoom(room, gameTestIp)
	require.NoError(t, err)
	for i, id := range []int{2512, 2513} {
		_, err = JoinGameRoom(room, id, fmt.Sprintf("192.0.2.%d", i+1), nil)
		require.NoError(t, err)
	}
	openingErr := errors.New("invalid opening")
	_, err = JoinGameRoom(room, 2514, "192.0.2.3", func([]GameRoomSeat) (GameRoomStart, error) {
		return GameRoomStart{}, openingErr
	})
	assert.ErrorIs(t, err, openingErr)
	assert.Equal(t, 100, gameBalance(t, 2514), "开局失败回滚押注与座位")
	_, err = JoinGameRoom(room, 2514, "192.0.2.3", func(seats []GameRoomSeat) (GameRoomStart, error) {
		require.Len(t, seats, 4)
		for i, seat := range seats {
			assert.Equal(t, i, seat.Seat)
			assert.Equal(t, 2511+i, seat.UserId)
		}
		return GameRoomStart{State: "opening", ToMove: 2, Deadline: 123}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, GameRoomStatusPlaying, room.Status)
	assert.Equal(t, 4, room.Players)
	assert.Equal(t, 2, room.ToMove)
	assert.ErrorIs(t, LeaveGameRoom(room, 2514), ErrGameRoomChanged)

	// 单人最多输 20，但赢家可以赢到多人的押注；两个赢家分别抽成，全部余额加抽成守恒。
	require.NoError(t, FinishGameRoom(room, GameRoomResult{Nets: []int{30, 10, -20, -20}, WinnerSeat: 0, Winners: []int{0, 1}}, GameRoomEndWin, "finished", "2026-10-10"))
	assert.Equal(t, 4, room.Rake)
	assert.Equal(t, 2511, room.WinnerId)
	seats, err := GetGameRoomSeats([]int{room.Id})
	require.NoError(t, err)
	totalBalance := room.Rake
	for i, expected := range []int{27, 9, -20, -20} {
		assert.Equal(t, expected, seats[room.Id][i].Net)
		account, err := GetGameCoinAccount(2511 + i)
		require.NoError(t, err)
		assert.Equal(t, 100+expected, account.Balance)
		assert.Equal(t, expected, account.TotalWagerNet)
		assert.Equal(t, 20, account.TotalWagered)
		net, plays, err := GetGameDayStats(2511+i, room.Game, "2026-10-10")
		require.NoError(t, err)
		assert.Equal(t, expected, net)
		assert.Equal(t, 1, plays)
		totalBalance += account.Balance
	}
	assert.Equal(t, 400, totalBalance)
}

func TestGameRoomRejectsUnsafeAmountsAndSettlement(t *testing.T) {
	truncateTables(t)
	for _, test := range []struct {
		name   string
		stake  int
		escrow int
		rake   int
	}{
		{"negative stake", -1, 0, 0},
		{"insufficient escrow", 2, 1, 0},
		{"free stake with escrow", 0, 1, 0},
		{"oversized prize", 1, common.MaxQuota / 2, 0},
		{"negative rake", 1, 1, -1},
		{"excess rake", 1, 1, 101},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CreateGameRoom(&GameRoom{Game: "gomoku", HostId: 2521, Seats: 2, Stake: test.stake, Escrow: test.escrow, RakePercent: test.rake}, gameTestIp)
			assert.ErrorIs(t, err, ErrGameCoinAmountInvalid)
		})
	}
	seedGameUser(t, 2521, 100)
	seedGameUser(t, 2522, 100)
	room := createTestGameRoom(t, 2521, 20, 0)
	_, err := JoinGameRoom(room, 2522, gameTestOtherIp, startTestGameRoom)
	require.NoError(t, err)
	for _, result := range []GameRoomResult{
		{Nets: []int{20}, WinnerSeat: 0},
		{Nets: []int{20, -19}, WinnerSeat: 0},
		{Nets: []int{21, -21}, WinnerSeat: 0},
		{Nets: []int{common.MaxQuota, -common.MaxQuota}, WinnerSeat: 0},
		{Nets: []int{20, -20}, WinnerSeat: 1},
		{Nets: []int{20, -20}, WinnerSeat: 2},
		{Nets: []int{0, 0}, WinnerSeat: 0},
	} {
		assert.Error(t, FinishGameRoom(room, result, GameRoomEndWin, "{}", "2026-10-10"))
	}
	// 调用方即使传入被改过的押注、抽成，也不能改变数据库里已经锁定的结算条件。
	room.Escrow, room.RakePercent = common.MaxQuota-1, -100
	assert.Error(t, FinishGameRoom(room, GameRoomResult{Nets: []int{40, -40}, WinnerSeat: 0}, GameRoomEndWin, "{}", "2026-10-10"))
	assert.Equal(t, 80, gameBalance(t, 2521))
	assert.Equal(t, 80, gameBalance(t, 2522))
	require.NoError(t, FinishGameRoom(room, GameRoomResult{Nets: []int{20, -20}, WinnerSeat: 0}, GameRoomEndWin, "{}", "2026-10-10"))
	assert.Equal(t, 120, gameBalance(t, 2521))
}

func TestGameRoomSettlementOverflowRollsBackAllPlayers(t *testing.T) {
	truncateTables(t)
	seedGameUser(t, 2531, 20)
	seedGameUser(t, 2532, 20)
	room := createTestGameRoom(t, 2531, 20, 0)
	_, err := JoinGameRoom(room, 2532, gameTestOtherIp, startTestGameRoom)
	require.NoError(t, err)
	// 房间期间来自其他来源的入账使赢家余额接近上限。即使前一个玩家已经写过账，也必须全部回滚。
	require.NoError(t, DB.Model(&GameCoinAccount{}).Where("user_id = ?", 2532).Update("balance", common.MaxQuota-20).Error)
	assert.ErrorIs(t, FinishGameRoom(room, GameRoomResult{Nets: []int{-20, 20}, WinnerSeat: 1}, GameRoomEndWin, "{}", "2026-10-10"), ErrGameCoinAmountInvalid)
	latest, err := GetGameRoom(room.Id)
	require.NoError(t, err)
	assert.Equal(t, GameRoomStatusPlaying, latest.Status)
	assert.Equal(t, room.Version, latest.Version)
	account, err := GetGameCoinAccount(2531)
	require.NoError(t, err)
	assert.Zero(t, account.TotalWagerNet)
	seats, err := GetGameRoomSeats([]int{room.Id})
	require.NoError(t, err)
	for _, seat := range seats[room.Id] {
		assert.Zero(t, seat.Net)
	}
	_, plays, err := GetGameDayStats(2531, room.Game, "2026-10-10")
	require.NoError(t, err)
	assert.Zero(t, plays)
}

func TestGameRoomMaximumEscrowRemainsConserved(t *testing.T) {
	for _, count := range []int{2, GameRoomMaxSeats} {
		t.Run(fmt.Sprintf("%d seats", count), func(t *testing.T) {
			truncateTables(t)
			escrow := (common.MaxQuota - 1) / count
			for i := 0; i < count; i++ {
				seedGameUser(t, 2541+i, escrow)
			}
			room := &GameRoom{Game: "cards", HostId: 2541, Stake: 1, Escrow: escrow, Seats: count, RakePercent: 100}
			_, err := CreateGameRoom(room, gameTestIp)
			require.NoError(t, err)
			for i := 1; i < count; i++ {
				_, err = JoinGameRoom(room, 2541+i, fmt.Sprintf("192.0.2.%d", i), startTestGameRoom)
				require.NoError(t, err)
			}
			nets := make([]int, count)
			nets[0] = escrow * (count - 1)
			for i := 1; i < count; i++ {
				nets[i] = -escrow
			}
			require.NoError(t, FinishGameRoom(room, GameRoomResult{Nets: nets, WinnerSeat: 0}, GameRoomEndWin, "{}", "2026-10-10"))
			total := room.Rake
			assert.GreaterOrEqual(t, room.Rake, 0)
			for i := 0; i < count; i++ {
				balance := gameBalance(t, 2541+i)
				assert.GreaterOrEqual(t, balance, 0)
				total += balance
			}
			assert.Equal(t, escrow*count, total)
		})
	}
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
