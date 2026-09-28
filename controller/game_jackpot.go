package controller

import (
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameJackpot = "jackpot"

const (
	// jackpotStaleSeconds 是只有一位玩家的一轮最多等多久，过了就原样退回投入。
	jackpotStaleSeconds = 10 * 60
	jackpotRecentLimit  = 10
)

// pickJackpotTicket 用密码学安全的随机数从 [0, tickets) 里抽一张奖券。测试里替换成固定的奖券。
var pickJackpotTicket = func(tickets int) (int, error) {
	ticket, err := rand.Int(rand.Reader, big.NewInt(int64(tickets)))
	if err != nil {
		return 0, err
	}
	return int(ticket.Int64()), nil
}

func init() {
	registerGame(&gameDefinition{
		Key:  gameJackpot,
		Kind: operation_setting.GameKindParty,
		Info: jackpotInfo,
		Summarize: func(ctx *gameContext, summary gin.H, counts gameDayCounts) {
			round, err := model.GetOpenGameJackpotRound()
			if err == nil && round != nil {
				summary["pot"] = round.Pot
				summary["players"] = round.Players
			}
		},
		// 投入的游戏币押在奖池里，游戏关闭后也要让这一轮投了币的玩家回来看开奖。
		Resumable: func(ctx *gameContext) bool {
			entered, err := model.HasOpenGameJackpotEntry(ctx.UserId)
			return err == nil && entered
		},
		Routes: func(group *gin.RouterGroup) {
			group.POST("/enter", middleware.GameSpinRateLimit("jackpot-enter"), EnterJackpot)
		},
	})
}

// resolveGameJackpot 给到点的一轮开奖，退回一直只有一位玩家、放太久的一轮。没人访问时由系统任务定期调用。
func resolveGameJackpot() error {
	if _, err := model.DrawGameJackpot(pickJackpotTicket, gameCenterDay()); err != nil {
		return err
	}
	_, err := model.RefundStaleGameJackpot(common.GetTimestamp() - jackpotStaleSeconds)
	return err
}

// jackpotInfo 返回奖池页的信息：当前一轮的奖池、倒计时与每位玩家的投入和中奖概率，最近几轮的开奖结果，以及今天的输赢。
// 页面每隔几秒刷新一次，到点的一轮在这里开奖。
func jackpotInfo(ctx *gameContext) (gin.H, error) {
	if err := resolveGameJackpot(); err != nil {
		return nil, err
	}
	rule := ctx.Setting.Jackpot
	round, err := jackpotRoundView(ctx.UserId)
	if err != nil {
		return nil, err
	}
	recent, err := jackpotRecentRounds(ctx.UserId)
	if err != nil {
		return nil, err
	}
	net, plays, err := model.GetGameDayStats(ctx.UserId, gameJackpot, ctx.Day)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"rule": gin.H{
			"max_bet":       rule.MaxBet,
			"round_seconds": rule.RoundSeconds,
			"rake_percent":  rule.RakePercent,
			"stale_seconds": jackpotStaleSeconds,
		},
		"round":       round,
		"recent":      recent,
		"net_today":   net,
		"plays_today": plays,
		"server_time": common.GetTimestamp(),
	}, nil
}

// jackpotRoundView 是开放中的一轮：每位玩家按第一次投入的先后排列，带着投入合计；没有开放的一轮时为 nil。
func jackpotRoundView(userId int) (gin.H, error) {
	round, err := model.GetOpenGameJackpotRound()
	if err != nil || round == nil {
		return nil, err
	}
	entries, err := model.ListGameJackpotEntries(round.Id)
	if err != nil {
		return nil, err
	}
	order := make([]int, 0)
	totals := make(map[int]int)
	for _, entry := range entries {
		if _, seen := totals[entry.UserId]; !seen {
			order = append(order, entry.UserId)
		}
		totals[entry.UserId] += entry.Amount
	}
	names, err := model.GetGameDisplayNames(order)
	if err != nil {
		return nil, err
	}
	players := make([]gin.H, 0, len(order))
	for _, id := range order {
		players = append(players, gin.H{"name": names[id], "amount": totals[id], "own": id == userId})
	}
	return gin.H{
		"id":           round.Id,
		"pot":          round.Pot,
		"deadline":     round.Deadline,
		"rake_percent": round.RakePercent,
		"created_at":   round.CreatedAt,
		"players":      players,
		"mine":         totals[userId],
	}, nil
}

// jackpotRecentRounds 是最近开奖的几轮：赢家、奖池、赢家拿到的游戏币与赢家的投入。
func jackpotRecentRounds(userId int) ([]gin.H, error) {
	rounds, err := model.ListRecentGameJackpotRounds(jackpotRecentLimit)
	if err != nil {
		return nil, err
	}
	winners := make([]int, 0, len(rounds))
	for _, round := range rounds {
		winners = append(winners, round.WinnerId)
	}
	names, err := model.GetGameDisplayNames(winners)
	if err != nil {
		return nil, err
	}
	recent := make([]gin.H, 0, len(rounds))
	for _, round := range rounds {
		recent = append(recent, gin.H{
			"id":           round.Id,
			"pot":          round.Pot,
			"payout":       round.Payout,
			"rake":         round.Rake,
			"players":      round.Players,
			"winner":       names[round.WinnerId],
			"winner_stake": round.WinnerStake,
			"ticket":       round.Ticket,
			"own":          round.WinnerId == userId,
			"drawn_at":     round.DrawnAt,
		})
	}
	return recent, nil
}

type jackpotEnterRequest struct {
	Amount int `json:"amount"`
}

// EnterJackpot 往当前一轮的奖池里投入游戏币。每人每轮的投入合计不超过每轮上限。
func EnterJackpot(c *gin.Context) {
	if !requireGameEnabled(c, gameJackpot) {
		return
	}
	rule := operation_setting.GetGameSetting().Jackpot
	var req jackpotEnterRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !validWagerBet(c, req.Amount, 1, rule.MaxBet) {
		return
	}
	if err := resolveGameJackpot(); err != nil {
		respondGameError(c, gameJackpot, err)
		return
	}
	_, _, err := model.EnterGameJackpot(c.GetInt("id"), req.Amount, model.GameJackpotEntryRule{
		MaxBet:       rule.MaxBet,
		RoundSeconds: rule.RoundSeconds,
		RakePercent:  rule.RakePercent,
	}, gameRewardIpKey(c.ClientIP()))
	switch {
	case errors.Is(err, model.ErrGameJackpotClosed):
		common.ApiErrorI18n(c, i18n.MsgGameJackpotClosed)
		return
	case errors.Is(err, model.ErrGameJackpotLimit):
		common.ApiErrorI18n(c, i18n.MsgGameJackpotLimit, map[string]any{"Max": rule.MaxBet})
		return
	case err != nil:
		respondGameError(c, gameJackpot, err)
		return
	}
	gameInfoHandler(findGameDefinition(gameJackpot))(c)
}
