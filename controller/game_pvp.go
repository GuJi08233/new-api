package controller

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 对战游戏的公共流程：房间的开设、加入、快速匹配、长轮询、落子、认输、超时判负与取消，以及和服务端 AI 的人机对战。
// 各游戏只提供规则引擎(pvpEngine)，在自己的文件里用 registerPvpGame 登记。

const (
	// pvpPollTimeout 是一次长轮询最多等多久，远低于常见反向代理的空闲超时。
	pvpPollTimeout = 25 * time.Second
	// pvpPollInterval 是 gameRoomWatchers 读库查看房间版本的间隔：本进程内的变化会立即唤醒，别的节点上的变化靠它发现。
	pvpPollInterval = time.Second
	// pvpWaitingSeconds 是房间等待对手的最长时间，过了就关闭并退回押注。
	pvpWaitingSeconds = 30 * 60
	pvpLobbyLimit     = 50
	pvpRecentLimit    = 10
	pvpRoomCodeLength = 8
	// bcrypt 最多接受 72 字节，按 UTF-8 字节数校验，避免密码被截断。
	pvpRoomPasswordMaxBytes = 72
	// pvpAiLevels 是人机对战的难度数：1 简单、2 普通、3 困难。
	pvpAiLevels = 3
	// pvpAiMaxReplies 是 AI 一次最多连走几步，远多于任何一盘棋的步数。
	pvpAiMaxReplies = 400
	// pvpMoveAttempts 是双方同时出招时一招最多尝试写入几次。
	pvpMoveAttempts = 3
	// pvpAutoMoveSeconds 是托管中的座位轮到时，AI 替他走之前留出的秒数，让桌上的人看清上一手。
	pvpAutoMoveSeconds = 2
	// pvpAutoLevel 是托管用的 AI 难度(普通)。
	pvpAutoLevel = 2
)

// pvpAiQueueTimeout 是 AI 搜索名额都占满时一步棋最多排队多久：等太久的请求直接回应失败，玩家稍后再走，而不是越积越多、
// 在客户端早已放弃之后还占着名额空算。测试里调短它。
var pvpAiQueueTimeout = 5 * time.Second

// pvpEngine 是一个对战游戏的规则引擎。状态以 JSON 保存在房间或人机对局里，引擎只负责解释它。
// 房间座位 0 是房主，桌人数由游戏声明；人机对战里玩家坐座位 0，其余座位都是 AI。
type pvpEngine interface {
	// New 返回开局状态，first 是先走的座位。
	New(first int) (pvpState, error)
	// Play 让 seat 走一步。不是它走、走法不合规则时返回 errPvpIllegalMove。
	Play(state []byte, seat int, move json.RawMessage) (pvpState, error)
	// View 返回给 seat 看的状态，隐藏对手还没公开的信息；seat 为 -1 表示旁观者。
	View(state []byte, seat int) (any, error)
	// AiMove 为 seat 选一步，level 为 1 到 pvpAiLevels，只能用 seat 看得到的信息。不支持人机对战的游戏返回错误。多人游戏的
	// 托管也用它替超时的玩家走，要在几毫秒内算完。
	AiMove(state []byte, seat int, level int) (json.RawMessage, error)
}

// pvpState 是引擎给出的一个局面。
type pvpState struct {
	Data []byte
	// ToMove 是轮到哪个座位走，-1 表示同时出招。
	ToMove int
	Over   bool
	// Winner 是 Over 时的赢家座位，-1 表示平局；多人游戏里是先结束的座位，每个座位的输赢由 pvpMultiplayer.Scores 给出。
	Winner int
}

var errPvpIllegalMove = errors.New("illegal move")

// 悔棋的几种拒绝：没有自己走过的棋可撤或棋局已经结束、已经有一个请求在等、刚被拒绝过、没有等自己回应的请求。
var (
	errPvpUndoUnavailable = errors.New("nothing to undo")
	errPvpUndoPending     = errors.New("undo request pending")
	errPvpUndoDeclined    = errors.New("undo request declined")
	errPvpUndoNoRequest   = errors.New("no undo request to answer")
)

// errPvpAiBusy 表示 AI 搜索名额一直占满，这一步排队超过了 pvpAiQueueTimeout。
var errPvpAiBusy = errors.New("ai search queue is full")

// pvpAiSearches 限制同时进行的 AI 搜索：一次搜索要占满一个 CPU 核几毫秒到上百毫秒，同时搜索的不超过核数的一半，
// 其余的排队等候，别的接口不受拖累。
var pvpAiSearches = make(chan struct{}, max(runtime.NumCPU()/2, 1))

// pvpSimultaneous 由双方同时出招的引擎实现(石头剪刀布)。对手先出招只改变玩家看不到的状态，玩家的一招不取决于它，
// 由引擎按回合核对，所以落子不要求带着最新的房间版本；两人几乎同时提交、后写的一方撞上版本冲突时重新读出房间再走一次。
type pvpSimultaneous interface {
	simultaneous()
}

// pvpDuel 由双人对战的引擎实现：一方超时或认输判负。
type pvpDuel interface {
	// Timeout 返回轮到走的一方超时之后的赢家座位，-1 表示平局(例如双方同时出招都没出)。
	Timeout(state []byte) (int, error)
}

// pvpUndoable 由支持悔棋的双人对战引擎实现(象棋)。一方提出，另一方同意后撤回到提出的一方上一次走棋之前，人机对战里电脑
// 总是同意；悔棋的次数与费用由引擎记在局面里。
type pvpUndoable interface {
	// UndoAsk 记下 seat 想悔棋，返回对方同意之后要收 seat 多少游戏币。不能悔棋时返回 errPvpUndo* 之一。
	UndoAsk(state []byte, seat int) (pvpState, model.GameCoins, error)
	// UndoAnswer 让 seat 回应对方的悔棋请求，accept 为真时撤回棋步。
	UndoAnswer(state []byte, seat int, accept bool) (pvpUndo, error)
}

// pvpUndo 是回应悔棋请求之后的局面。同意时 Payer 是提出悔棋的座位，Cost 是要收他的游戏币(免费时为 0)，Steps 是撤回了几步；
// 拒绝时 Payer 为 -1。
type pvpUndo struct {
	State pvpState
	Payer int
	Cost  model.GameCoins
	Steps int
}

// pvpMultiplayer 由多人对战的引擎实现。一个人超时不判他的队伍输(队友没有过错)，而是转为托管、由 AI 替他走，多人对局也不能
// 认输，要离开可以托管。
type pvpMultiplayer interface {
	// Seats 是桌人数，坐满才开局。
	Seats() int
	// MaxLoss 是一个座位最多输掉几倍押注：入座时按它押下游戏币，结算时退回没输掉的部分。
	MaxLoss() int
	// Scores 返回自然结束的一局里每个座位输赢几倍押注，合计为 0。
	Scores(state []byte) ([]int, error)
}

// pvpGame 是登记过的一个对战游戏。name 是写进审计日志的游戏名。duel 与 table 有且只有一个不为空。
type pvpGame struct {
	key    string
	name   string
	engine pvpEngine
	duel   pvpDuel
	table  pvpMultiplayer
	seats  int
	// maxLoss 是一个座位最多输掉几倍押注，双人对战是 1。
	maxLoss int
}

var pvpGames = map[string]*pvpGame{}

// pvpAiGame 是人机对战的对局与计数用的游戏标识，和房间对战的输赢计数分开。
func pvpAiGame(game string) string {
	return game + "_ai"
}

// registerPvpGame 登记一个对战游戏：房间接口按房间的游戏找到引擎，人机对战的接口挂在 /api/game/<key>/ai 下。
func registerPvpGame(key string, name string, engine pvpEngine) {
	game := &pvpGame{key: key, name: name, engine: engine, seats: 2, maxLoss: 1}
	if table, ok := engine.(pvpMultiplayer); ok {
		game.table, game.seats, game.maxLoss = table, table.Seats(), table.MaxLoss()
	} else if duel, ok := engine.(pvpDuel); ok {
		game.duel = duel
	}
	if game.duel == nil && game.table == nil || game.seats < 2 || game.seats > model.GameRoomMaxSeats || game.maxLoss < 1 {
		panic("invalid pvp game: " + key)
	}
	pvpGames[key] = game
	registerGame(&gameDefinition{
		Key:  key,
		Kind: operation_setting.GameKindPvp,
		Info: func(ctx *gameContext) (gin.H, error) {
			return pvpInfo(ctx, game)
		},
		Summarize: func(ctx *gameContext, summary gin.H, counts gameDayCounts) {
			rule, _ := ctx.Setting.PvpRuleOf(key)
			ai := pvpAiGame(key)
			summary["ai_enabled"] = rule.Ai.Enabled
			summary["ai_daily_rewards"] = rule.Ai.DailyRewards
			summary["ai_rewards_today"] = max(counts.user[ai].FreePlays, counts.ip[ai].FreePlays)
			summary["waiting_rooms"] = ctx.waitingRooms()[key]
			summary["seats"] = game.seats
		},
		Resumable: func(ctx *gameContext) bool {
			// 游戏中心列表要给每个关闭的对战游戏问一遍，查的都是玩家所有进行中的房间：同一个请求里只查一次。
			cached, ok := ctx.Get("game_active_rooms")
			if !ok {
				active, err := model.GetUserActiveGameRooms(ctx.UserId)
				if err != nil {
					return false
				}
				cached = active
				ctx.Set("game_active_rooms", active)
			}
			for _, room := range cached.([]*model.GameRoom) {
				if room.Game == key {
					return true
				}
			}
			return false
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/ai", func(c *gin.Context) { getPvpAi(c, game) })
			group.POST("/ai/start", middleware.GamePlayRateLimit(key+"-ai-start"), func(c *gin.Context) { startPvpAi(c, game) })
			group.POST("/ai/move", middleware.GameAiRateLimit(key+"-ai-move"), func(c *gin.Context) { movePvpAi(c, game) })
			group.POST("/ai/resign", middleware.GamePlayRateLimit(key+"-ai-resign"), func(c *gin.Context) { resignPvpAi(c, game) })
			if _, ok := engine.(pvpUndoable); ok {
				group.POST("/ai/undo", middleware.GamePlayRateLimit(key+"-ai-undo"), func(c *gin.Context) { undoPvpAi(c, game) })
			}
		},
	})
}

// waitingRooms 返回各对战游戏在大厅里等待对手的公开房间数，同一个请求里只查一次。
func (ctx *gameContext) waitingRooms() map[string]int {
	if cached, ok := ctx.Get("game_waiting_rooms"); ok {
		return cached.(map[string]int)
	}
	counts, err := model.CountWaitingGameRooms()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to count game rooms: %v", err))
	}
	ctx.Set("game_waiting_rooms", counts)
	return counts
}

// randomSeat 用密码学安全的随机数决定先走的座位。
func randomSeat(seats int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(seats)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

// gameRoomWatchers 让长轮询在房间有变化时立即返回。本进程里的变化由 notify 立即唤醒；别的节点上的变化由后台的 poll 发现：
// 每隔 pvpPollInterval 用一条查询读出所有被等待房间的版本，唤醒看到的版本已经过时的等待者。等多少人，每个进程每秒也只查
// 一次库，而不是每个等待者各查一次；没有等待者时 poll 退出，下次有人等时再启动。
type gameRoomWatchers struct {
	mu sync.Mutex
	// waiters 按房间记着每个等待者看到的房间版本。
	waiters map[int]map[chan struct{}]int
	polling bool
}

var gameRoomWatch = &gameRoomWatchers{waiters: map[int]map[chan struct{}]int{}}

// watch 登记一个看到了 version 版房间的等待者，房间有变化时往返回的通道里发一个信号。
func (w *gameRoomWatchers) watch(roomId int, version int) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	set := w.waiters[roomId]
	if set == nil {
		set = map[chan struct{}]int{}
		w.waiters[roomId] = set
	}
	set[ch] = version
	if !w.polling {
		w.polling = true
		go w.poll()
	}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		delete(set, ch)
		if len(set) == 0 {
			delete(w.waiters, roomId)
		}
		w.mu.Unlock()
	}
}

func (w *gameRoomWatchers) notify(roomId int) {
	w.mu.Lock()
	for ch := range w.waiters[roomId] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	w.mu.Unlock()
}

// poll 是在后台读库、发现别的节点上变化的循环，见 gameRoomWatchers。
func (w *gameRoomWatchers) poll() {
	ticker := time.NewTicker(pvpPollInterval)
	defer ticker.Stop()
	for range ticker.C {
		w.mu.Lock()
		if len(w.waiters) == 0 {
			w.polling = false
			w.mu.Unlock()
			return
		}
		ids := make([]int, 0, len(w.waiters))
		for id := range w.waiters {
			ids = append(ids, id)
		}
		w.mu.Unlock()
		versions, err := model.GetGameRoomVersions(ids)
		if err != nil {
			common.SysError(fmt.Sprintf("failed to poll game room versions: %v", err))
			continue
		}
		w.mu.Lock()
		for _, id := range ids {
			// 读不到的房间也唤醒，让等待者自己读出错误返回。
			latest, found := versions[id]
			for ch, seen := range w.waiters[id] {
				if found && latest <= seen {
					continue
				}
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
		w.mu.Unlock()
	}
}

// resolveGameRoom 处理到期的房间：对局中这一步已超时的，双人对战判超时一方负，多人对战由 AI 替他走并转为托管；等太久没人
// 加入的关闭并退回押注。房间有变化时返回 true，room 已更新为最新状态。
func resolveGameRoom(room *model.GameRoom) (bool, error) {
	now := common.GetTimestamp()
	var err error
	switch {
	case room.Status == model.GameRoomStatusPlaying && room.Deadline > 0 && now > room.Deadline:
		game := pvpGames[room.Game]
		if game == nil {
			return false, fmt.Errorf("unknown pvp game %s", room.Game)
		}
		if game.table != nil {
			err = autoPlayGameRoom(room, game)
			break
		}
		winnerSeat, timeoutErr := game.duel.Timeout([]byte(room.State))
		if timeoutErr != nil {
			return false, timeoutErr
		}
		err = finishPvpRoom(room, game, duelScores(winnerSeat), model.GameRoomEndTimeout, room.State)
	case room.Status == model.GameRoomStatusWaiting && now-room.CreatedAt > pvpWaitingSeconds:
		err = model.CancelGameRoom(room, model.GameRoomEndExpired)
	default:
		return false, nil
	}
	if errors.Is(err, model.ErrGameRoomChanged) {
		// 另一个请求刚处理过，读出最新状态。
		latest, readErr := model.GetGameRoom(room.Id)
		if readErr != nil {
			return false, readErr
		}
		*room = *latest
		return true, nil
	}
	if err != nil {
		return false, err
	}
	gameRoomWatch.notify(room.Id)
	return true, nil
}

// autoPlayGameRoom 让 AI 替这一步超时的座位走，并把这个座位转为托管：之后轮到它时 pvpAutoMoveSeconds 秒后由 AI 替他走，
// 玩家自己走一步或取消托管就恢复。所有座位都在托管时桌上已经没人在玩，一口气替他们下完。AI 出错(不应发生)时这一局作废，
// 按平局退回所有人的押注，房间不会卡住、押注不会一直锁着。
func autoPlayGameRoom(room *model.GameRoom, game *pvpGame) error {
	next := pvpState{Data: []byte(room.State), ToMove: room.ToMove}
	auto, everyone := room.AutoSeats, 1<<room.Seats-1
	for moves := 0; ; moves++ {
		seat := next.ToMove
		var err error
		if seat < 0 || seat >= room.Seats || moves == pvpAiMaxReplies {
			err = fmt.Errorf("seat %d to move after %d auto moves", seat, moves)
		} else {
			var move json.RawMessage
			if move, err = game.engine.AiMove(next.Data, seat, pvpAutoLevel); err == nil {
				next, err = game.engine.Play(next.Data, seat, move)
			}
		}
		if err != nil {
			common.SysError(fmt.Sprintf("auto play failed in game room %d: %v", room.Id, err))
			return finishPvpRoom(room, game, make([]int, room.Seats), model.GameRoomEndDraw, room.State)
		}
		auto |= 1 << seat
		if next.Over {
			return settlePvpRoom(room, game, next)
		}
		if auto != everyone {
			deadline := common.GetTimestamp() + pvpMoveSeconds(room, next.ToMove, auto)
			return model.UpdateGameRoomState(room, string(next.Data), next.ToMove, deadline, auto)
		}
	}
}

// pvpMoveSeconds 是轮到 toMove 时这一步有几秒：托管中的座位 pvpAutoMoveSeconds 秒后由 AI 替他走，其他座位有完整的每步限时。
// 房间的截止时间总是这一步开始的时间加上它，打开或取消托管时据此算回这一步从哪里开始。
func pvpMoveSeconds(room *model.GameRoom, toMove int, autoSeats int) int64 {
	if toMove >= 0 && autoSeats&(1<<toMove) != 0 {
		return pvpAutoMoveSeconds
	}
	return int64(room.MoveSeconds)
}

// scores 返回自然结束的一局里每个座位输赢几倍押注：多人对战由规则给出，双人对战见 duelScores。
func (g *pvpGame) scores(next pvpState) ([]int, error) {
	if g.table != nil {
		return g.table.Scores(next.Data)
	}
	return duelScores(next.Winner), nil
}

// duelScores 是双人对战 winner 获胜时双方输赢几倍押注：赢家 1、输家 -1，winner 为 -1 是平局。
func duelScores(winner int) []int {
	scores := make([]int, 2)
	if winner >= 0 {
		scores[winner], scores[1-winner] = 1, -1
	}
	return scores
}

// settlePvpRoom 结算按规则下完的一局，有人赢时结束原因是 win，否则是 draw。
func settlePvpRoom(room *model.GameRoom, game *pvpGame, next pvpState) error {
	scores, err := game.scores(next)
	if err != nil {
		return err
	}
	reason := model.GameRoomEndDraw
	if slices.ContainsFunc(scores, func(score int) bool { return score > 0 }) {
		reason = model.GameRoomEndWin
	}
	return finishPvpRoom(room, game, scores, reason, string(next.Data))
}

// finishPvpRoom 结束对局并结算：scores 是每个座位输赢几倍押注，合计为 0，为正的座位获胜，全为 0 是平局。一个座位最多输掉
// game.maxLoss 倍(入座时押下的)，规则给出的分数越界时拒绝结算。
func finishPvpRoom(room *model.GameRoom, game *pvpGame, scores []int, reason string, state string) error {
	if room.Seats != game.seats || len(scores) != room.Seats {
		return errors.New("invalid game room result")
	}
	result := model.GameRoomResult{Nets: make([]model.GameCoins, room.Seats), WinnerSeat: -1}
	for seat, score := range scores {
		if score < -game.maxLoss || score > game.maxLoss*(room.Seats-1) {
			return fmt.Errorf("game room score %d out of range", score)
		}
		result.Nets[seat] = model.GameCoins(score) * room.Stake
		if score > 0 {
			result.Winners = append(result.Winners, seat)
			if result.WinnerSeat < 0 || score > scores[result.WinnerSeat] {
				result.WinnerSeat = seat
			}
		}
	}
	return model.FinishGameRoom(room, result, reason, state, gameCenterDay())
}

// pvpRoomParticipants 批量读出房间的座位和昵称，列表查询不随房间数逐条查库。
func pvpRoomParticipants(rooms []*model.GameRoom) (map[int][]model.GameRoomSeat, map[int]string, error) {
	ids := make([]int, 0, len(rooms))
	for _, room := range rooms {
		ids = append(ids, room.Id)
	}
	seats, err := model.GetGameRoomSeats(ids)
	if err != nil {
		return nil, nil, err
	}
	users := make([]int, 0, len(rooms)*2)
	for _, members := range seats {
		for _, member := range members {
			users = append(users, member.UserId)
		}
	}
	names, err := model.GetGameDisplayNames(users)
	return seats, names, err
}

// pvpRoomMembers 是房间里的玩家：座位、昵称、结算后的输赢与胜负，以及是否在托管。
func pvpRoomMembers(room *model.GameRoom, seats []model.GameRoomSeat, names map[int]string) []gin.H {
	members := make([]gin.H, 0, len(seats))
	for _, seat := range seats {
		members = append(members, gin.H{
			"seat": seat.Seat, "user_id": seat.UserId, "name": names[seat.UserId], "net": seat.Net, "result": seat.Result,
			"auto": room.AutoSeats&(1<<seat.Seat) != 0,
		})
	}
	return members
}

// pvpRoomView 是房间给 viewer 看的样子：对手还没公开的信息由引擎隐藏，私密房间的邀请码只给房主。
func pvpRoomView(room *model.GameRoom, viewer int) (gin.H, error) {
	seats, names, err := pvpRoomParticipants([]*model.GameRoom{room})
	if err != nil {
		return nil, err
	}
	seat := -1
	for _, member := range seats[room.Id] {
		if member.UserId == viewer {
			seat = member.Seat
		}
	}
	members := pvpRoomMembers(room, seats[room.Id], names)
	view := gin.H{
		"id":           room.Id,
		"game":         room.Game,
		"status":       room.Status,
		"stake":        room.Stake,
		"escrow":       room.Escrow,
		"seats":        room.Seats,
		"player_count": room.Players,
		"locked":       room.Locked(),
		"private":      room.Private,
		"version":      room.Version,
		"seat":         seat,
		"members":      members,
		"host_id":      room.HostId,
		"to_move":      room.ToMove,
		"deadline":     room.Deadline,
		"server_time":  common.GetTimestamp(),
		"move_seconds": room.MoveSeconds,
		"auto_seats":   room.AutoSeats,
		"rake_percent": room.RakePercent,
		"winner_id":    room.WinnerId,
		"end_reason":   room.EndReason,
		"rake":         room.Rake,
		"created_at":   room.CreatedAt,
	}
	if seat == 0 && room.Status == model.GameRoomStatusWaiting {
		view["code"] = room.Code
	}
	if room.State != "" {
		game := pvpGames[room.Game]
		if game == nil {
			return nil, fmt.Errorf("unknown pvp game %s", room.Game)
		}
		state, err := game.engine.View([]byte(room.State), seat)
		if err != nil {
			return nil, err
		}
		view["state"] = state
	}
	return view, nil
}

func respondGameRoom(c *gin.Context, room *model.GameRoom, extra gin.H) {
	view, err := pvpRoomView(room, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for key, value := range extra {
		view[key] = value
	}
	common.ApiSuccess(c, view)
}

// pvpLobby 是大厅：某个游戏里等待对手的公开房间，以及玩家自己在这个游戏里等待中或对局中的房间。
func pvpLobby(userId int, game string) (gin.H, error) {
	rooms, err := model.ListWaitingPublicGameRooms([]string{game}, common.GetTimestamp()-pvpWaitingSeconds, pvpLobbyLimit)
	if err != nil {
		return nil, err
	}
	seats, names, err := pvpRoomParticipants(rooms)
	if err != nil {
		return nil, err
	}
	list := make([]gin.H, 0, len(rooms))
	for _, room := range rooms {
		list = append(list, gin.H{
			"id":           room.Id,
			"stake":        room.Stake,
			"escrow":       room.Escrow,
			"seats":        room.Seats,
			"player_count": room.Players,
			"locked":       room.Locked(),
			"members":      pvpRoomMembers(room, seats[room.Id], names),
			"host":         names[room.HostId],
			"own":          room.HostId == userId,
			"created_at":   room.CreatedAt,
		})
	}
	lobby := gin.H{"rooms": list, "active": nil}
	active, err := model.GetUserActiveGameRooms(userId)
	if err != nil {
		return nil, err
	}
	for _, room := range active {
		if room.Game != game {
			continue
		}
		if _, err := resolveGameRoom(room); err != nil {
			return nil, err
		}
		if room.Status == model.GameRoomStatusWaiting || room.Status == model.GameRoomStatusPlaying {
			view, err := pvpRoomView(room, userId)
			if err != nil {
				return nil, err
			}
			lobby["active"] = view
		}
		break
	}
	return lobby, nil
}

// pvpInfo 返回对战游戏页的信息：规则、大厅、最近的对局，以及人机对战的领奖规则、进度与进行中的一局。
func pvpInfo(ctx *gameContext, game *pvpGame) (gin.H, error) {
	rule, _ := ctx.Setting.PvpRuleOf(game.key)
	lobby, err := pvpLobby(ctx.UserId, game.key)
	if err != nil {
		return nil, err
	}
	recent, err := pvpRecentRooms(ctx.UserId, game.key)
	if err != nil {
		return nil, err
	}
	info := gin.H{
		"rule": gin.H{
			"seats":        game.seats,
			"max_loss":     game.maxLoss,
			"max_stake":    rule.MaxStake,
			"rake_percent": rule.RakePercent,
			"move_seconds": rule.MoveSeconds,
			"ai_enabled":   rule.Ai.Enabled,
		},
		"lobby":  lobby,
		"recent": recent,
	}
	if rule.Ai.Enabled {
		ai, err := skillInfo(ctx, pvpAiGame(game.key), rule.Ai)
		if err != nil {
			return nil, err
		}
		session, err := model.GetPlayingGameSession(ctx.UserId, pvpAiGame(game.key))
		if err != nil {
			return nil, err
		}
		// 过期的一局已经走不了也认不了输，不再恢复，玩家直接回到大厅。
		if session != nil && common.GetTimestamp()-session.CreatedAt <= skillSessionTTL {
			if ai["session"], err = pvpAiView(game, session); err != nil {
				return nil, err
			}
		}
		info["ai"] = ai
	}
	return info, nil
}

// pvpRecentRooms 是玩家在这个游戏里最近结束的对局：同桌玩家、押注与输赢。
func pvpRecentRooms(userId int, game string) ([]gin.H, error) {
	rooms, err := model.ListUserFinishedGameRooms(userId, game, pvpRecentLimit)
	if err != nil {
		return nil, err
	}
	seats, names, err := pvpRoomParticipants(rooms)
	if err != nil {
		return nil, err
	}
	recent := make([]gin.H, 0, len(rooms))
	for _, room := range rooms {
		net, mySeat, result := model.GameCoins(0), -1, "draw"
		for _, member := range seats[room.Id] {
			if member.UserId == userId {
				net = member.Net
				mySeat = member.Seat
				if member.Result != "" {
					result = member.Result
				}
			}
		}
		recent = append(recent, gin.H{
			"id":          room.Id,
			"seat":        mySeat,
			"stake":       room.Stake,
			"escrow":      room.Escrow,
			"net":         net,
			"members":     pvpRoomMembers(room, seats[room.Id], names),
			"rake":        room.Rake,
			"result":      result,
			"end_reason":  room.EndReason,
			"finished_at": room.FinishedAt,
		})
	}
	return recent, nil
}

func registerGameRoomRoutes(group *gin.RouterGroup) {
	group.GET("", middleware.GamePollRateLimit("rooms"), ListGameRooms)
	group.POST("", middleware.GamePlayRateLimit("room-create"), CreateGameRoom)
	group.POST("/match", middleware.GamePlayRateLimit("room-match"), MatchGameRoom)
	group.POST("/join", middleware.GamePlayRateLimit("room-join"), JoinGameRoomByCode)
	group.GET("/invite", middleware.GamePollRateLimit("room-invite"), PreviewGameRoomInvite)
	group.GET("/:id", middleware.GamePollRateLimit("room-poll"), PollGameRoom)
	group.POST("/:id/join", middleware.GamePlayRateLimit("room-join"), JoinGameRoomById)
	group.POST("/:id/move", middleware.GameActionRateLimit("room-move"), MoveGameRoom)
	group.POST("/:id/resign", middleware.GamePlayRateLimit("room-resign"), ResignGameRoom)
	group.POST("/:id/auto", middleware.GamePlayRateLimit("room-auto"), AutoGameRoom)
	group.POST("/:id/undo", middleware.GamePlayRateLimit("room-undo"), AskUndoGameRoom)
	group.POST("/:id/undo/answer", middleware.GamePlayRateLimit("room-undo"), AnswerUndoGameRoom)
	group.POST("/:id/cancel", middleware.GamePlayRateLimit("room-cancel"), CancelGameRoom)
	group.POST("/:id/leave", middleware.GamePlayRateLimit("room-leave"), LeaveGameRoom)
}

type gameRoomCreateRequest struct {
	Game     string          `json:"game"`
	Stake    model.GameCoins `json:"stake"`
	Private  bool            `json:"private"`
	Seats    *int            `json:"seats"`
	Password string          `json:"password"`
}

type gameRoomJoinRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

type gameRoomMoveRequest struct {
	Version int             `json:"version"`
	Move    json.RawMessage `json:"move"`
}

type gameRoomAutoRequest struct {
	Auto *bool `json:"auto"`
}

type gameRoomUndoAnswerRequest struct {
	Accept *bool `json:"accept"`
}

// PreviewGameRoomInvite 只读取邀请码对应的入座条件，不扣币、不占座或暴露私密局面。
func PreviewGameRoomInvite(c *gin.Context) {
	code := c.Query("code")
	if len(code) != pvpRoomCodeLength {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	room, err := model.GetWaitingGameRoomByCode(code)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	if _, ok := pvpOpenRule(c, room.Game); !ok {
		return
	}
	if common.GetTimestamp()-room.CreatedAt > pvpWaitingSeconds {
		respondGameRoomError(c, model.ErrGameRoomNotFound)
		return
	}
	common.ApiSuccess(c, gin.H{
		"game": room.Game, "stake": room.Stake, "escrow": room.Escrow,
		"seats": room.Seats, "player_count": room.Players,
		"locked": room.Locked(), "status": room.Status,
	})
}

// ListGameRooms 返回某个对战游戏的大厅。
func ListGameRooms(c *gin.Context) {
	game := c.Query("game")
	if pvpGames[game] == nil {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	lobby, err := pvpLobby(c.GetInt("id"), game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, lobby)
}

// pvpOpenRule 读出可以开房或加入的对战游戏的规则：游戏不存在或没开放时写好错误响应并返回 false。
func pvpOpenRule(c *gin.Context, game string) (operation_setting.PvpRule, bool) {
	setting := operation_setting.GetGameSetting()
	rule, ok := setting.PvpRuleOf(game)
	if !ok || pvpGames[game] == nil || !setting.GameEnabled(game) {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return rule, false
	}
	return rule, true
}

// validPvpStake 检查押注是 0(友谊赛)或 [0.01, MaxStake] 之内、最多两位小数，不是时写好错误响应并返回 false。
func validPvpStake(c *gin.Context, stake model.GameCoins, rule operation_setting.PvpRule) bool {
	if stake == 0 || validGameStake(stake, rule.MaxStake) {
		return true
	}
	common.ApiErrorI18n(c, i18n.MsgGameRoomStakeInvalid, map[string]any{"Max": rule.MaxStake})
	return false
}

// respondGameRoomError 把房间操作的常见错误换成给玩家看的提示；其他错误记日志后提示操作失败。
func respondGameRoomError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrGameRoomNotFound):
		common.ApiErrorI18n(c, i18n.MsgGameRoomNotFound)
	case errors.Is(err, model.ErrGameRoomActive):
		common.ApiErrorI18n(c, i18n.MsgGameRoomActive)
	case errors.Is(err, model.ErrGameRoomChanged), errors.Is(err, model.ErrGameRoomFull):
		common.ApiErrorI18n(c, i18n.MsgGameRoomChanged)
	case errors.Is(err, model.ErrGameRoomSameIp):
		common.ApiErrorI18n(c, i18n.MsgGameRoomSameIp)
	case errors.Is(err, model.ErrGameCoinInsufficient):
		common.ApiErrorI18n(c, i18n.MsgGameCoinInsufficient)
	case errors.Is(err, errPvpIllegalMove):
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
	case errors.Is(err, errPvpUndoUnavailable):
		common.ApiErrorI18n(c, i18n.MsgGameUndoUnavailable)
	case errors.Is(err, errPvpUndoPending):
		common.ApiErrorI18n(c, i18n.MsgGameUndoPending)
	case errors.Is(err, errPvpUndoDeclined):
		common.ApiErrorI18n(c, i18n.MsgGameUndoDeclined)
	case errors.Is(err, errPvpUndoNoRequest):
		common.ApiErrorI18n(c, i18n.MsgGameUndoNoRequest)
	default:
		common.SysError(fmt.Sprintf("game room operation failed for user %d: %v", c.GetInt("id"), err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
	}
}

// createPvpRoom 按规则开一个等待对手的房间并押下押注，返回 nil 时已写好错误响应。
func createPvpRoom(c *gin.Context, game string, rule operation_setting.PvpRule, stake model.GameCoins, private bool, password string) (*model.GameRoom, model.GameCoins) {
	room := &model.GameRoom{
		Game:        game,
		Stake:       stake,
		Escrow:      stake * model.GameCoins(pvpGames[game].maxLoss),
		Seats:       pvpGames[game].seats,
		Private:     private,
		Code:        common.GetRandomString(pvpRoomCodeLength),
		HostId:      c.GetInt("id"),
		RakePercent: rule.RakePercent,
		MoveSeconds: rule.MoveSeconds,
	}
	if password != "" {
		hash, err := common.Password2Hash(password)
		if err != nil {
			respondGameRoomError(c, err)
			return nil, 0
		}
		room.PasswordHash = hash
	}
	balance, err := model.CreateGameRoom(room, gameRewardIpKey(c.ClientIP()))
	if err != nil {
		respondGameRoomError(c, err)
		return nil, 0
	}
	return room, balance
}

// CreateGameRoom 开一个等待对手的房间。私密房间不在大厅列出，把带邀请码的链接发给朋友就能加入。
func CreateGameRoom(c *gin.Context) {
	var req gameRoomCreateRequest
	if err := decodeGameRequest(c, &req); err != nil || len(req.Password) > pvpRoomPasswordMaxBytes {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, ok := pvpOpenRule(c, req.Game)
	if !ok || !validPvpStake(c, req.Stake, rule) {
		return
	}
	if req.Seats != nil && *req.Seats != pvpGames[req.Game].seats {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	room, balance := createPvpRoom(c, req.Game, rule, req.Stake, req.Private, req.Password)
	if room == nil {
		return
	}
	respondGameRoom(c, room, gin.H{"balance": balance})
}

// joinPvpRoom 以玩家身份加入等待中的房间，坐满后开局，首个行动座位随机决定。押注对局不能和同一 IP 下的账号对战。
// 返回 false 时已写好错误响应。
func joinPvpRoom(c *gin.Context, room *model.GameRoom, password string) bool {
	userId := c.GetInt("id")
	if _, ok := pvpOpenRule(c, room.Game); !ok {
		return false
	}
	if room.Seats != pvpGames[room.Game].seats {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return false
	}
	if _, err := resolveGameRoom(room); err != nil {
		respondGameRoomError(c, err)
		return false
	}
	ip := gameRewardIpKey(c.ClientIP())
	switch {
	case room.HostId == userId:
		common.ApiErrorI18n(c, i18n.MsgGameRoomSelf)
		return false
	case room.Status != model.GameRoomStatusWaiting:
		common.ApiErrorI18n(c, i18n.MsgGameRoomStarted)
		return false
	case room.Locked() && !common.ValidatePasswordAndHash(password, room.PasswordHash):
		common.ApiErrorI18n(c, i18n.MsgGameRoomPassword)
		return false
	}
	balance, err := model.JoinGameRoom(room, userId, ip, func(seats []model.GameRoomSeat) (model.GameRoomStart, error) {
		return startPvpRoom(room, seats)
	})
	if err != nil {
		respondGameRoomError(c, err)
		return false
	}
	gameRoomWatch.notify(room.Id)
	respondGameRoom(c, room, gin.H{"balance": balance})
	return true
}

// startPvpRoom 在所有规则要求的座位坐满后，于最后一位玩家入座的事务中创建局面。
func startPvpRoom(room *model.GameRoom, seats []model.GameRoomSeat) (model.GameRoomStart, error) {
	game := pvpGames[room.Game]
	if game == nil || room.Seats != game.seats || len(seats) != game.seats {
		return model.GameRoomStart{}, errors.New("invalid game room seats")
	}
	first, err := randomSeat(room.Seats)
	if err != nil {
		return model.GameRoomStart{}, err
	}
	state, err := game.engine.New(first)
	if err != nil {
		return model.GameRoomStart{}, err
	}
	return model.GameRoomStart{
		State: string(state.Data), ToMove: state.ToMove,
		Deadline: common.GetTimestamp() + int64(room.MoveSeconds),
	}, nil
}

// JoinGameRoomById 加入大厅里的一个公开房间。私密房间只能凭邀请码加入，按编号找不到。
func JoinGameRoomById(c *gin.Context) {
	var req gameRoomJoinRequest
	if err := decodeGameRequest(c, &req); (err != nil && !errors.Is(err, io.EOF)) || len(req.Password) > pvpRoomPasswordMaxBytes {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	room, err := model.GetGameRoom(id)
	if err == nil && room.Private {
		err = model.ErrGameRoomNotFound
	}
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	joinPvpRoom(c, room, req.Password)
}

// JoinGameRoomByCode 凭邀请码加入房间，私密房间只能这样加入。
func JoinGameRoomByCode(c *gin.Context) {
	var req gameRoomJoinRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Code == "" || len(req.Password) > pvpRoomPasswordMaxBytes {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	room, err := model.GetWaitingGameRoomByCode(req.Code)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	joinPvpRoom(c, room, req.Password)
}

// MatchGameRoom 快速匹配：加入大厅里押注相同、等得最久的房间，没有时开一个公开房间等别人来。
func MatchGameRoom(c *gin.Context) {
	var req gameRoomCreateRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Password != "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, ok := pvpOpenRule(c, req.Game)
	if !ok || !validPvpStake(c, req.Stake, rule) {
		return
	}
	if req.Seats != nil && *req.Seats != pvpGames[req.Game].seats {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	excludeIp := ""
	if req.Stake > 0 {
		excludeIp = gameRewardIpKey(c.ClientIP())
	}
	// 同时匹配的几个人可能挑中同一个房间，被人抢先时换下一个。
	for attempt := 0; attempt < 3; attempt++ {
		room, err := model.FindGameRoomMatch(req.Game, req.Stake, pvpGames[req.Game].seats, userId, excludeIp)
		if err != nil {
			respondGameRoomError(c, err)
			return
		}
		if room == nil {
			break
		}
		if _, err := resolveGameRoom(room); err != nil {
			respondGameRoomError(c, err)
			return
		}
		if room.Status != model.GameRoomStatusWaiting {
			continue
		}
		balance, err := model.JoinGameRoom(room, userId, gameRewardIpKey(c.ClientIP()), func(seats []model.GameRoomSeat) (model.GameRoomStart, error) {
			return startPvpRoom(room, seats)
		})
		if errors.Is(err, model.ErrGameRoomChanged) || errors.Is(err, model.ErrGameRoomFull) {
			continue
		}
		if err != nil {
			respondGameRoomError(c, err)
			return
		}
		gameRoomWatch.notify(room.Id)
		respondGameRoom(c, room, gin.H{"balance": balance})
		return
	}
	room, balance := createPvpRoom(c, req.Game, rule, req.Stake, false, "")
	if room == nil {
		return
	}
	respondGameRoom(c, room, gin.H{"balance": balance})
}

// loadPlayerGameRoom 读出房间并处理到期的情况；viewer 不是房间里的玩家时回应错误。返回 nil 时已写好错误响应。
func loadPlayerGameRoom(c *gin.Context) *model.GameRoom {
	id, _ := strconv.Atoi(c.Param("id"))
	room, err := model.GetGameRoom(id)
	if err != nil {
		respondGameRoomError(c, err)
		return nil
	}
	seats, err := model.GetGameRoomSeats([]int{room.Id})
	if err != nil {
		respondGameRoomError(c, err)
		return nil
	}
	seat := -1
	for _, member := range seats[room.Id] {
		if member.UserId == c.GetInt("id") {
			seat = member.Seat
			break
		}
	}
	if seat < 0 {
		common.ApiErrorI18n(c, i18n.MsgGameRoomNotPlayer)
		return nil
	}
	c.Set("game_room_seat", seat)
	if _, err := resolveGameRoom(room); err != nil {
		respondGameRoomError(c, err)
		return nil
	}
	return room
}

// PollGameRoom 长轮询房间：房间版本比 after 新时立即返回，否则等到有变化、这一步超时或最多 pvpPollTimeout。
// 不带 after 时立即返回当前状态。
func PollGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	after, err := strconv.Atoi(c.DefaultQuery("after", "-1"))
	if err != nil {
		after = -1
	}
	giveUp := time.Now().Add(pvpPollTimeout)
	for room.Version <= after && time.Now().Before(giveUp) {
		wait := time.Until(giveUp)
		if room.Status == model.GameRoomStatusPlaying && room.Deadline > 0 {
			// 这一步快到截止时间时提前醒来，超时判负要及时告诉双方。
			wait = min(wait, max(time.Until(time.Unix(room.Deadline+1, 0)), 10*time.Millisecond))
		}
		changed, cancel := gameRoomWatch.watch(room.Id, room.Version)
		select {
		case <-changed:
		case <-time.After(wait):
		case <-c.Request.Context().Done():
			cancel()
			return
		}
		cancel()
		// 等待期间玩家可能已从另一页面离座，重新检查成员身份。
		room = loadPlayerGameRoom(c)
		if room == nil {
			return
		}
	}
	respondGameRoom(c, room, nil)
}

// MoveGameRoom 走一步。请求带着玩家看到的房间版本，期间对手已经走了或对局已结束时拒绝，玩家刷新后再走。
func MoveGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	var req gameRoomMoveRequest
	if err := decodeGameRequest(c, &req); err != nil || len(req.Move) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	seat := c.GetInt("game_room_seat")
	game := pvpGames[room.Game]
	if game == nil || room.Seats != game.seats {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	_, simultaneous := game.engine.(pvpSimultaneous)
	for attempt := 1; ; attempt++ {
		switch {
		case room.Status != model.GameRoomStatusPlaying || (req.Version != room.Version && !simultaneous):
			common.ApiErrorI18n(c, i18n.MsgGameRoomChanged)
			return
		case room.ToMove >= 0 && room.ToMove != seat:
			common.ApiErrorI18n(c, i18n.MsgGameRoomNotYourTurn)
			return
		}
		next, err := game.engine.Play([]byte(room.State), seat, req.Move)
		if err != nil {
			respondGameRoomError(c, err)
			return
		}
		if next.Over {
			err = settlePvpRoom(room, game, next)
		} else {
			// 自己走了一步就不再托管。
			auto := room.AutoSeats &^ (1 << seat)
			err = model.UpdateGameRoomState(room, string(next.Data), next.ToMove, common.GetTimestamp()+pvpMoveSeconds(room, next.ToMove, auto), auto)
		}
		if errors.Is(err, model.ErrGameRoomChanged) && simultaneous && attempt < pvpMoveAttempts {
			if room, err = model.GetGameRoom(room.Id); err != nil {
				respondGameRoomError(c, err)
				return
			}
			continue
		}
		if err != nil {
			respondGameRoomError(c, err)
			return
		}
		gameRoomWatch.notify(room.Id)
		respondGameRoom(c, room, nil)
		return
	}
}

// ResignGameRoom 认输，对手获胜。多人游戏不能认输(会连累队友)，要离开可以托管。
func ResignGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	if room.Status != model.GameRoomStatusPlaying {
		respondGameRoom(c, room, nil)
		return
	}
	game := pvpGames[room.Game]
	if game == nil || room.Seats != game.seats {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	if game.table != nil {
		common.ApiErrorI18n(c, i18n.MsgGameRoomResignTeam)
		return
	}
	seat := c.GetInt("game_room_seat")
	if err := finishPvpRoom(room, game, duelScores(1-seat), model.GameRoomEndResign, room.State); err != nil {
		respondGameRoomError(c, err)
		return
	}
	gameRoomWatch.notify(room.Id)
	respondGameRoom(c, room, nil)
}

// AutoGameRoom 打开或取消自己座位的托管，只有多人对战可以托管。托管不改变这一步从哪里开始计时：轮到自己时打开托管，AI 在
// 这一步开始 pvpAutoMoveSeconds 秒后替自己走，已经过了就马上走；取消托管拿回这一步剩下的时间，来回切换也拖不了时间。
func AutoGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	var req gameRoomAutoRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Auto == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if game := pvpGames[room.Game]; game == nil || game.table == nil {
		common.ApiErrorI18n(c, i18n.MsgGameRoomAutoUnsupported)
		return
	}
	if room.Status != model.GameRoomStatusPlaying {
		respondGameRoom(c, room, nil)
		return
	}
	seat := c.GetInt("game_room_seat")
	auto := room.AutoSeats &^ (1 << seat)
	if *req.Auto {
		auto |= 1 << seat
	}
	if auto != room.AutoSeats {
		start := room.Deadline - pvpMoveSeconds(room, room.ToMove, room.AutoSeats)
		if err := model.UpdateGameRoomState(room, room.State, room.ToMove, start+pvpMoveSeconds(room, room.ToMove, auto), auto); err != nil {
			respondGameRoomError(c, err)
			return
		}
		gameRoomWatch.notify(room.Id)
		if _, err := resolveGameRoom(room); err != nil {
			respondGameRoomError(c, err)
			return
		}
	}
	respondGameRoom(c, room, nil)
}

// loadUndoGameRoom 读出可以悔棋的对局中的房间：游戏不支持悔棋时回应错误，房间不在对局中时回应房间现在的样子。返回 nil
// 时已写好响应。
func loadUndoGameRoom(c *gin.Context) (*model.GameRoom, pvpUndoable) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return nil, nil
	}
	game := pvpGames[room.Game]
	undo, ok := pvpUndoable(nil), false
	if game != nil && room.Seats == game.seats {
		undo, ok = game.engine.(pvpUndoable)
	}
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgGameUndoUnsupported)
		return nil, nil
	}
	if room.Status != model.GameRoomStatusPlaying {
		respondGameRoom(c, room, nil)
		return nil, nil
	}
	return room, undo
}

// AskUndoGameRoom 提出悔棋，等对手同意。要付费时先看余额够不够，免得对手同意了才发现付不起；计时照常，不因为请求暂停。
func AskUndoGameRoom(c *gin.Context) {
	room, undo := loadUndoGameRoom(c)
	if room == nil {
		return
	}
	next, cost, err := undo.UndoAsk([]byte(room.State), c.GetInt("game_room_seat"))
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	if cost > 0 {
		account, err := model.GetGameCoinAccount(c.GetInt("id"))
		if err != nil {
			respondGameRoomError(c, err)
			return
		}
		if account.Balance < cost {
			respondGameRoomError(c, model.ErrGameCoinInsufficient)
			return
		}
	}
	if err := model.UpdateGameRoomState(room, string(next.Data), room.ToMove, room.Deadline, room.AutoSeats); err != nil {
		respondGameRoomError(c, err)
		return
	}
	gameRoomWatch.notify(room.Id)
	respondGameRoom(c, room, nil)
}

// AnswerUndoGameRoom 回应对手的悔棋请求。同意时撤回棋步、轮到对手重新走并给他完整的每步限时，同一个事务里扣下他这次悔棋的
// 费用；他的余额这时已经不够时悔棋不执行，请求留着，可以拒绝或接着走棋。
func AnswerUndoGameRoom(c *gin.Context) {
	room, undo := loadUndoGameRoom(c)
	if room == nil {
		return
	}
	var req gameRoomUndoAnswerRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Accept == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	answer, err := undo.UndoAnswer([]byte(room.State), c.GetInt("game_room_seat"), *req.Accept)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	next := answer.State
	if answer.Payer < 0 {
		err = model.UpdateGameRoomState(room, string(next.Data), room.ToMove, room.Deadline, room.AutoSeats)
	} else {
		var seats map[int][]model.GameRoomSeat
		if seats, err = model.GetGameRoomSeats([]int{room.Id}); err == nil {
			payer := 0
			for _, member := range seats[room.Id] {
				if member.Seat == answer.Payer {
					payer = member.UserId
				}
			}
			deadline := common.GetTimestamp() + pvpMoveSeconds(room, next.ToMove, room.AutoSeats)
			_, err = model.UndoGameRoomState(room, string(next.Data), next.ToMove, deadline, room.AutoSeats, payer, answer.Cost)
		}
	}
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	gameRoomWatch.notify(room.Id)
	respondGameRoom(c, room, nil)
}

// LeaveGameRoom 离开还没开局的房间并退回押注，房主离开时关闭房间。
func LeaveGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	if err := model.LeaveGameRoom(room, c.GetInt("id")); err != nil {
		respondGameRoomError(c, err)
		return
	}
	gameRoomWatch.notify(room.Id)
	account, err := model.GetGameCoinAccount(c.GetInt("id"))
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	respondGameRoom(c, room, gin.H{"balance": account.Balance})
}

// CancelGameRoom 房主关闭还没等到对手的房间，押注退回。
func CancelGameRoom(c *gin.Context) {
	room := loadPlayerGameRoom(c)
	if room == nil {
		return
	}
	if room.HostId != c.GetInt("id") {
		common.ApiErrorI18n(c, i18n.MsgGameRoomNotHost)
		return
	}
	if room.Status == model.GameRoomStatusWaiting {
		if err := model.CancelGameRoom(room, model.GameRoomEndCancel); err != nil {
			respondGameRoomError(c, err)
			return
		}
		gameRoomWatch.notify(room.Id)
	}
	respondGameRoom(c, room, nil)
}

// pvpAiState 是人机对战保存在对局里的状态。玩家坐座位 0，其余座位都是 AI。Result 是结束时玩家的胜负(win、loss、draw)，
// 团队游戏里队友先出完也算玩家赢。
type pvpAiState struct {
	Level  int             `json:"level"`
	Board  json.RawMessage `json:"board"`
	ToMove int             `json:"to_move"`
	Over   bool            `json:"over"`
	Winner int             `json:"winner"`
	Result string          `json:"result,omitempty"`
	Moves  int             `json:"moves"`
}

// pvpAiDetail 是人机对战一局的成绩明细。
type pvpAiDetail struct {
	Level  int    `json:"level"`
	Winner int    `json:"winner"`
	Result string `json:"result"`
	Moves  int    `json:"moves"`
}

type pvpAiStartRequest struct {
	Level int `json:"level"`
}

type pvpAiMoveRequest struct {
	SessionId int             `json:"session_id"`
	Version   int             `json:"version"`
	Move      json.RawMessage `json:"move"`
}

// apply 记下引擎给出的新局面，结束时按规则给出的输赢算出玩家的结果。
func (s *pvpAiState) apply(game *pvpGame, next pvpState) error {
	s.Board, s.ToMove, s.Over, s.Winner = next.Data, next.ToMove, next.Over, next.Winner
	s.Moves++
	if !s.Over {
		return nil
	}
	scores, err := game.scores(next)
	if err != nil {
		return err
	}
	s.Result = "draw"
	switch {
	case scores[0] > 0:
		s.Result = "win"
	case scores[0] < 0:
		s.Result = "loss"
	}
	return nil
}

// aiReply 让 AI 一直走到轮到玩家或对局结束，返回 AI 的最后一步。玩家无子可下只能停一手时(黑白棋)AI 会连走几步，多人游戏里
// 几个 AI 座位依次走；每一步都要改变局面，走满 pvpAiMaxReplies 步还没交还玩家说明引擎有问题。ctx 是请求的上下文，客户端
// 断开后不再排队。
func (s *pvpAiState) aiReply(ctx context.Context, game *pvpGame) (json.RawMessage, error) {
	var move json.RawMessage
	for replies := 0; !s.Over && s.ToMove != 0; replies++ {
		if replies == pvpAiMaxReplies || s.ToMove < 0 {
			return nil, fmt.Errorf("ai cannot hand the turn back after %d moves (to move %d)", replies, s.ToMove)
		}
		var err error
		move, err = pvpAiSearchMove(ctx, game.engine, s.Board, s.ToMove, s.Level)
		if err != nil {
			return nil, err
		}
		next, err := game.engine.Play(s.Board, s.ToMove, move)
		if err != nil {
			return nil, fmt.Errorf("ai chose an illegal move %s: %w", move, err)
		}
		if err := s.apply(game, next); err != nil {
			return nil, err
		}
	}
	return move, nil
}

// pvpAiSearchMove 占一个 pvpAiSearches 名额让 AI 替 seat 想一步。名额占满时排队，最多等 pvpAiQueueTimeout，请求取消时不再等；
// 名额在搜索结束时归还，引擎出错甚至 panic 也不会少掉一个。
func pvpAiSearchMove(ctx context.Context, engine pvpEngine, board []byte, seat int, level int) (json.RawMessage, error) {
	queued := time.NewTimer(pvpAiQueueTimeout)
	defer queued.Stop()
	select {
	case pvpAiSearches <- struct{}{}:
	case <-queued.C:
		return nil, errPvpAiBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-pvpAiSearches }()
	return engine.AiMove(board, seat, level)
}

// pvpAiView 是人机对战一局给玩家看的样子。
func pvpAiView(game *pvpGame, session *model.GameSession) (gin.H, error) {
	var state pvpAiState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		return nil, err
	}
	board, err := game.engine.View(state.Board, 0)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"status":     session.Status,
		"level":      state.Level,
		"seat":       0,
		"seats":      game.seats,
		"to_move":    state.ToMove,
		"over":       state.Over,
		"winner":     state.Winner,
		"result":     state.Result,
		"moves":      state.Moves,
		"state":      board,
	}, nil
}

func getPvpAi(c *gin.Context, game *pvpGame) {
	var session *model.GameSession
	var err error
	if requested, present := c.GetQuery("session_id"); present {
		id, parseErr := strconv.Atoi(requested)
		if parseErr != nil || id < 1 {
			common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
			return
		}
		// 丢失落子或认输的响应时按原局恢复，包括已经结算的一局；只能查询自己的同类游戏。
		session, err = model.GetUserGameSession(c.GetInt("id"), pvpAiGame(game.key), id)
		if errors.Is(err, model.ErrGameSessionNotFound) {
			common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
			return
		}
	} else {
		session, err = model.GetPlayingGameSession(c.GetInt("id"), pvpAiGame(game.key))
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session != nil && session.Status == model.GameSessionStatusFinished {
		rule, _ := operation_setting.GetGameSetting().PvpRuleOf(game.key)
		respondPvpAiSettled(c, game, rule.Ai, session, nil, nil)
		return
	}
	if session == nil || session.Status != model.GameSessionStatusPlaying || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	view, err := pvpAiView(game, session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

// pvpAiRule 读出人机对战的领奖规则：游戏没开放或没开放人机对战时写好错误响应并返回 false。
func pvpAiRule(c *gin.Context, game *pvpGame) (operation_setting.SkillRule, bool) {
	setting := operation_setting.GetGameSetting()
	rule, _ := setting.PvpRuleOf(game.key)
	if !setting.GameEnabled(game.key) {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return rule.Ai, false
	}
	if !rule.Ai.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGameAiDisabled)
		return rule.Ai, false
	}
	return rule.Ai, true
}

// startPvpAi 开一局人机对战。先走的一方随机决定，AI 先走时这里就替它走第一步。赢了能领奖，所以和技巧游戏一样要过人机验证。
func startPvpAi(c *gin.Context, game *pvpGame) {
	if _, ok := pvpAiRule(c, game); !ok {
		return
	}
	var req pvpAiStartRequest
	if err := decodeGameRequest(c, &req); err != nil || req.Level < 1 || req.Level > pvpAiLevels {
		common.ApiErrorI18n(c, i18n.MsgGameAiLevelInvalid)
		return
	}
	session := startSkillSession(c, pvpAiGame(game.key), func(uint32) (string, error) {
		first, err := randomSeat(game.seats)
		if err != nil {
			return "", err
		}
		initial, err := game.engine.New(first)
		if err != nil {
			return "", err
		}
		state := pvpAiState{Level: req.Level, Board: initial.Data, ToMove: initial.ToMove, Winner: -1}
		if _, err := state.aiReply(c.Request.Context(), game); err != nil {
			return "", err
		}
		data, err := common.Marshal(state)
		return string(data), err
	})
	if session == nil {
		return
	}
	view, err := pvpAiView(game, session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

// loadPvpAiSession 读出进行中的一局人机对战；已经结束时回应最后的局面与结算结果。返回 nil 时已写好响应。
func loadPvpAiSession(c *gin.Context, game *pvpGame, rule operation_setting.SkillRule, sessionId int) (*model.GameSession, *pvpAiState) {
	session, err := model.GetUserGameSession(c.GetInt("id"), pvpAiGame(game.key), sessionId)
	if errors.Is(err, model.ErrGameSessionNotFound) {
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return nil, nil
	}
	if err != nil {
		common.ApiError(c, err)
		return nil, nil
	}
	if session.Status != model.GameSessionStatusPlaying {
		respondPvpAiSettled(c, game, rule, session, nil, nil)
		return nil, nil
	}
	if common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiErrorI18n(c, i18n.MsgGameSessionExpired)
		return nil, nil
	}
	var state pvpAiState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		common.ApiError(c, err)
		return nil, nil
	}
	return session, &state
}

// respondPvpAiSettled 回应一局已经结束的人机对战：最后的局面加上结算结果。result 为空时按对局记录重新组装。
func respondPvpAiSettled(c *gin.Context, game *pvpGame, rule operation_setting.SkillRule, session *model.GameSession, result gin.H, aiMove json.RawMessage) {
	var err error
	if result == nil {
		if result, err = skillSettledView(c, session, rule); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	view, err := pvpAiView(game, session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": view, "ai_move": aiMove, "result": result})
}

// settlePvpAi 结算结束的一局人机对战：赢了按难度领奖，输了或和棋成绩为 0，seconds 只计已验证的玩家操作时长。
func settlePvpAi(c *gin.Context, game *pvpGame, rule operation_setting.SkillRule, session *model.GameSession, state *pvpAiState, aiMove json.RawMessage, seconds int) {
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	score := 0
	if state.Result == "win" {
		score = state.Level
	}
	result := settleSkillSession(c, game.name+"人机对战", rule, session, skillOutcome{
		Score:   score,
		Seconds: seconds,
		Detail:  pvpAiDetail{Level: state.Level, Winner: state.Winner, Result: state.Result, Moves: state.Moves},
		State:   string(data),
	})
	if result == nil {
		return
	}
	session.Status, session.State = model.GameSessionStatusFinished, string(data)
	respondPvpAiSettled(c, game, rule, session, result, aiMove)
}

// movePvpAi 玩家走一步，没结束时 AI 接着走，都在这一个请求里完成。
func movePvpAi(c *gin.Context, game *pvpGame) {
	rule, ok := pvpAiRule(c, game)
	if !ok {
		return
	}
	var req pvpAiMoveRequest
	if err := decodeGameRequest(c, &req); err != nil || len(req.Move) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	session, state := loadPvpAiSession(c, game, rule, req.SessionId)
	if session == nil {
		return
	}
	if req.Version != session.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	if state.ToMove != 0 {
		common.ApiErrorI18n(c, i18n.MsgGameRoomNotYourTurn)
		return
	}
	next, err := game.engine.Play(state.Board, 0, req.Move)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	if err := state.apply(game, next); err != nil {
		common.ApiError(c, err)
		return
	}
	aiMove, err := state.aiReply(c.Request.Context(), game)
	if err != nil {
		common.SysError(fmt.Sprintf("%s ai failed in session %d: %v", game.key, session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return
	}
	if state.Over {
		settlePvpAi(c, game, rule, session, state, aiMove, session.PlayedSeconds(common.GetTimestamp()))
		return
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
		respondGameError(c, pvpAiGame(game.key), err)
		return
	}
	view, err := pvpAiView(game, session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": view, "ai_move": aiMove})
}

// undoPvpAi 悔棋：撤回玩家上一步和电脑的应着，又轮到玩家走。电脑总是同意，费用和房间对局一样按次数收，和新局面在同一个
// 事务里扣下。
func undoPvpAi(c *gin.Context, game *pvpGame) {
	rule, ok := pvpAiRule(c, game)
	if !ok {
		return
	}
	undo := game.engine.(pvpUndoable)
	var req pvpAiMoveRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	session, state := loadPvpAiSession(c, game, rule, req.SessionId)
	if session == nil {
		return
	}
	if req.Version != session.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	if state.ToMove != 0 {
		common.ApiErrorI18n(c, i18n.MsgGameRoomNotYourTurn)
		return
	}
	asked, _, err := undo.UndoAsk(state.Board, 0)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	answer, err := undo.UndoAnswer(asked.Data, 1, true)
	if err != nil {
		respondGameRoomError(c, err)
		return
	}
	state.Board, state.ToMove, state.Winner = answer.State.Data, answer.State.ToMove, -1
	state.Moves = max(state.Moves-answer.Steps, 0)
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	balance, err := model.UndoGameSessionState(session, string(data), answer.Cost)
	if err != nil {
		respondGameError(c, pvpAiGame(game.key), err)
		return
	}
	view, err := pvpAiView(game, session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": view, "balance": balance})
}

// resignPvpAi 认输，这局按输了结算。
func resignPvpAi(c *gin.Context, game *pvpGame) {
	rule, ok := pvpAiRule(c, game)
	if !ok {
		return
	}
	var req pvpAiMoveRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	session, state := loadPvpAiSession(c, game, rule, req.SessionId)
	if session == nil {
		return
	}
	state.Over, state.Winner, state.Result = true, 1, "loss"
	// 认输不是走棋，只保留之前合法操作已经计入的时长，开局或末步之后的挂机时间不算。
	settlePvpAi(c, game, rule, session, state, nil, session.PlaySeconds)
}
