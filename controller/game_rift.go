package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 九幽秘境：问道里即时斗法的一轮(规则见 pkg/doufa 的 Run)，也是问道里按游戏币结算的无尽玩法。一轮的状态存在对局里，
// 战斗房由浏览器打完提交操作记录，服务端按房间的种子与开局重放核对，只认重放出来的结果。
//
// 游戏标识用 wendao：问道的开关、人机验证、领奖规则与每日领奖次数都挂在它上面，成绩是打通的重数。一轮结束时道韵、战绩、
// 带回宗门的材料与每日功课在结算的同一个事务里记进玩家的问道存档。道韵只能升级天赋，不能换成游戏币。
const riftGame = wendaoGame

// riftRunTTL 是一轮从开始到结算的最长时间：一轮要打很久，可以分几天玩完。
const riftRunTTL = 7 * 24 * 60 * 60

// riftGraceSeconds 是核对打一间房用时的余量：提交时间不能早于"帧数 ÷ 60"的八成减去这几秒(网络与页面切换的误差)。
const riftGraceSeconds = 2

type riftState struct {
	Run *doufa.Run `json:"run"`
}

func registerRiftRoutes(group *gin.RouterGroup) {
	group.GET("/state", getRiftRun)
	group.POST("/start", middleware.GamePlayRateLimit("rift-start"), startRiftRun)
	group.POST("/act", middleware.GameActionRateLimit("rift-act"), actRiftRun)
	group.POST("/talent", middleware.GamePlayRateLimit("rift-talent"), buyRiftTalent)
	group.POST("/shop/buy", middleware.GamePlayRateLimit("rift-shop"), buyRiftItem)
}

// riftInfo 是问道页里秘境板块的规则：领奖规则与今天的进度、局外成长(道韵、天赋、战绩、背包)、天赋表与道具商店。
func riftInfo(ctx *gameContext) (gin.H, error) {
	rule, _ := ctx.Setting.SkillRuleOf(riftGame)
	info, err := skillInfo(ctx, riftGame, rule)
	if err != nil {
		return nil, err
	}
	data, err := model.GetGameSave(ctx.UserId, wendao.SaveKey)
	if err != nil {
		return nil, err
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		return nil, err
	}
	wendaoPartGate(ctx, info, riftGame)
	info["meta"] = riftMetaView(save.Rift)
	info["catalog"] = gin.H{
		"talents":        doufa.Talents(),
		"floor_steps":    doufa.FloorSteps,
		"retreats":       doufa.BaseRetreats,
		"items_per_run":  doufa.ItemsPerRun,
		"max_item_stack": doufa.MaxItemStack,
	}
	ids, cents := make([]string, 0, len(doufa.ShopItems)), make([]int, 0, len(doufa.ShopItems))
	for _, item := range doufa.ShopItems {
		ids, cents = append(ids, item.Id), append(cents, item.Price)
	}
	if info["shop"], err = gameShopInfo(ctx.Setting, ids, cents); err != nil {
		return nil, err
	}
	return info, nil
}

// riftMetaView 是局外成长在页面上的样子。
func riftMetaView(meta *doufa.Meta) gin.H {
	return gin.H{"crystals": meta.Crystals, "earned": meta.Earned, "runs": meta.Runs, "talents": meta.Talents,
		"records": meta.Records, "items": meta.Items}
}

// parseRiftState 读出对局里的一轮。读不出来的(旧版卡牌秘境留下的、或损坏的)当作一轮按旧版规则开始的：只能放弃。
func parseRiftState(session *model.GameSession) *riftState {
	state := &riftState{}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil || state.Run == nil {
		return &riftState{Run: &doufa.Run{}}
	}
	return state
}

// riftView 是一轮在页面上的样子。deliver 为真时带着刚发出的房间(种子与开局)，页面据此开打；读取状态时已经发出的房间
// 不再给出种子，只能用一次退路拿到新的(见 doufa.Run.Public)。
func riftView(session *model.GameSession, run *doufa.Run, deliver bool) gin.H {
	view := gin.H{"session_id": session.Id, "version": session.Version, "status": session.Status, "run": run.Public(deliver),
		"max_hp": run.MaxHP(), "score": run.Score(), "floors": run.Floors()}
	if run.V != doufa.RunStateVersion {
		view["outdated"] = true
	}
	return view
}

func getRiftRun(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	userId := c.GetInt("id")
	best, err := model.GetGameBestScore(userId, riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	session, err := model.GetPlayingGameSession(userId, riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	save, err := loadWendaoSave(userId)
	if err != nil {
		respondSectError(c, err)
		return
	}
	answer := gin.H{"session": nil, "best": best, "travel": save.TravelView(common.GetTimestamp())}
	if session != nil && common.GetTimestamp()-session.CreatedAt <= riftRunTTL {
		answer["session"] = riftView(session, parseRiftState(session).Run, false)
	}
	common.ApiSuccess(c, answer)
}

func startRiftRun(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Class string `json:"class"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, err := doufa.NewRun(1, req.Class, nil); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 开新的一轮要先打完或放弃进行中的那一轮，过期的不算。
	userId := c.GetInt("id")
	playing, err := model.GetPlayingGameSession(userId, riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if playing != nil && common.GetTimestamp()-playing.CreatedAt <= riftRunTTL {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	if !passGamePlayTurnstile(c, operation_setting.GetGameSetting(), riftGame, userId) {
		return
	}
	seed, err := newGameSeed()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameStartFailed)
		return
	}
	var run *doufa.Run
	session, err := model.CreateGameSessionWithSave(userId, riftGame, seed, common.GetTimestamp()-riftRunTTL, wendao.SaveKey, func(data string) (string, error) {
		save, err := wendao.ParseSave(data)
		if err != nil {
			return "", err
		}
		if save.Hero != nil && save.Hero.Travel != nil {
			return "", wendao.ErrUnavailable
		}
		run, err = doufa.NewRun(seed, req.Class, save.Rift.Talents)
		if err != nil {
			return "", err
		}
		encoded, err := common.Marshal(riftState{Run: run})
		return string(encoded), err
	})
	if errors.Is(err, model.ErrGameRoundOpen) {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": riftView(session, run, false)})
}

// loadRiftSession 读出玩家要继续或结算的一轮。不存在或过期时回应错误；已经结算过的一轮回应第一次结算的结果(最后一步的
// 请求在网络上丢了再发一遍，拿到的是同一次结算，不会再发一次奖励)。返回 nil 时已写好响应。
func loadRiftSession(c *gin.Context, rule operation_setting.SkillRule, sessionId int) *model.GameSession {
	session, err := model.GetUserGameSession(c.GetInt("id"), riftGame, sessionId)
	if errors.Is(err, model.ErrGameSessionNotFound) {
		common.ApiErrorI18n(c, i18n.MsgGameSessionNotFound)
		return nil
	}
	if err != nil {
		respondGameFinishRetry(c)
		return nil
	}
	if session.Status != model.GameSessionStatusPlaying {
		result, err := skillSettledView(c, session, rule)
		if err != nil {
			respondGameFinishRetry(c)
			return nil
		}
		view := riftView(session, parseRiftState(session).Run, false)
		view["result"] = result
		common.ApiSuccess(c, gin.H{"session": view})
		return nil
	}
	if common.GetTimestamp()-session.CreatedAt > riftRunTTL {
		common.ApiErrorI18n(c, i18n.MsgGameSessionExpired)
		return nil
	}
	return session
}

// actRiftRun 走一步：enter 进第 choice 间房，pick 挑第 choice 样(法宝换第 slot 件)，clear 提交打完的房间(inputs 是
// 操作记录)，retreat 从没打完的房间里退出来(用一次退路，换个种子重新开始)，item 用背包里的一件道具，abandon 放弃。
// version 要与对局一致，另一个页面先走了这一步就拒绝。一轮结束(倒下或放弃)时按打通的重数结算。
func actRiftRun(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		Choice    int    `json:"choice"`
		Slot      int    `json:"slot"`
		Inputs    string `json:"inputs"`
		Item      string `json:"item"`
	}
	if decodeGameRequest(c, &req) != nil || req.Action == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(riftGame)
	session := loadRiftSession(c, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	state := parseRiftState(session)
	run := state.Run
	if run.V != doufa.RunStateVersion && req.Action != "abandon" {
		common.ApiErrorI18n(c, i18n.MsgGameDungeonOutdated)
		return
	}
	if req.Action == "item" {
		useRiftItem(c, session, state, req.Item)
		return
	}
	if req.Action == "enter" || req.Action == "retreat" {
		save, err := loadWendaoSave(c.GetInt("id"))
		if err != nil {
			respondSectError(c, err)
			return
		}
		if save.Hero != nil && save.Hero.Travel != nil {
			respondSectError(c, wendao.ErrUnavailable)
			return
		}
	}
	now := common.GetTimestamp()
	var err error
	switch req.Action {
	case "enter":
		err = run.Enter(req.Choice, now)
	case "pick":
		err = run.Pick(req.Choice, req.Slot)
	case "retreat":
		err = run.Retreat(now)
	case "abandon":
		err = run.Abandon()
	case "clear":
		if run.Room == nil {
			err = doufa.ErrRunAction
			break
		}
		inputs, decodeErr := doufa.DecodeInputs(req.Inputs)
		if decodeErr != nil {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
		fight, replayErr := doufa.Replay(run.Room.Seed, run.Room.Setup, inputs)
		// 操作记录要正好打到这间房结束；用时不能比帧数对应的时长快太多(加速或脚本)。
		if replayErr != nil || !fight.Over ||
			(now-run.Room.Issued+riftGraceSeconds)*5*doufa.FPS < int64(fight.Tick)*4 {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
		err = run.Clear(fight, now)
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if errors.Is(err, doufa.ErrRunAction) {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 这一步发出了房间(进房、退路重来、护命符复活)时，把种子与开局随回应带给页面。
	deliver := run.Room != nil && req.Action != "pick" && req.Action != "abandon"
	if !run.Over {
		if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
			respondGameError(c, riftGame, err)
			return
		}
		common.ApiSuccess(c, gin.H{"session": riftView(session, run, deliver)})
		return
	}
	settleRiftRun(c, rule, session, state, string(data), req.Action == "abandon")
}

// settleRiftRun 结算结束的一轮：按打通的重数算游戏币，并在同一个事务里把道韵、战绩、带回宗门的材料与每日功课记进存档。
// 记录先按结算前读到的存档算好写进明细，事务里再按锁住的存档重算：同一玩家同时只有一轮，两次结果相同。
func settleRiftRun(c *gin.Context, rule operation_setting.SkillRule, session *model.GameSession, state *riftState, data string, abandoned bool) {
	run := state.Run
	saved, err := model.GetGameSave(session.UserId, wendao.SaveKey)
	if err != nil {
		respondGameFinishRetry(c)
		return
	}
	preview, err := wendao.ParseSave(saved)
	if err != nil {
		respondGameFinishRetry(c)
		return
	}
	day := gameCenterDay()
	record := func(sv *wendao.Save) (int, wendao.Cost) {
		rollWendaoDaily(sv)
		// 一间房也没打通的一轮(开了就放弃)不算走完，也不攒道韵与功课。
		if run.Cleared == 0 {
			return 0, wendao.Cost{}
		}
		if sv.Sect != nil {
			sv.Sect.Progress(wendao.TaskAbyss, 1)
		}
		return sv.Rift.Settle(run), sv.GrantRiftLoot(run, day)
	}
	crystals, loot := record(preview)
	// 还没有宗门时没有库房可放，明细里不提带回的材料。
	var lootDetail any
	if preview.Sect != nil {
		lootDetail = loot
	}
	outcome := skillOutcome{
		Score:   run.Floors(),
		Seconds: min(session.PlayedSeconds(common.GetTimestamp()), run.Ticks/doufa.FPS+60),
		State:   data,
		Detail: gin.H{"class": run.Class, "floor": run.Floor, "step": run.Step, "cleared": run.Cleared, "kills": run.Kills,
			"stones": run.Stones, "abandoned": abandoned, "crystals": crystals, "loot": lootDetail},
		Save: &model.GameSaveUpdate{Game: wendao.SaveKey, Update: func(data string) (string, error) {
			return wendao.UpdateSave(data, func(sv *wendao.Save) error {
				record(sv)
				return nil
			})
		}},
	}
	result := settleSkillSession(c, "九幽秘境", rule, session, outcome)
	if result == nil {
		return
	}
	view := riftView(session, run, false)
	view["status"], view["result"], view["version"] = model.GameSessionStatusFinished, result, session.Version+1
	common.ApiSuccess(c, gin.H{"session": view})
}

// useRiftItem 在进行中的一轮里用掉背包里的一件道具：道具从存档里拿走与这一步的新局面在同一个事务里保存，道具不会白扣也不会白用。
// 道具不会让一轮结束，所以只保存新局面，不结算。
func useRiftItem(c *gin.Context, session *model.GameSession, state *riftState, item string) {
	data, err := model.GetGameSave(session.UserId, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		respondSectError(c, err)
		return
	}
	if save.Rift.Items[item] < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameShopNoItem)
		return
	}
	switch err := state.Run.UseItem(item); {
	case errors.Is(err, doufa.ErrItemLimit):
		common.ApiErrorI18n(c, i18n.MsgGameShopItemLimit)
		return
	case err != nil:
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	encoded, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var meta *doufa.Meta
	err = model.UpdateGameSessionAndSave(session, string(encoded), "", wendao.SaveKey, func(current string) (string, error) {
		return wendao.UpdateSave(current, func(sv *wendao.Save) error {
			meta = sv.Rift
			return sv.Rift.TakeItem(item)
		})
	})
	if errors.Is(err, doufa.ErrNoItem) {
		common.ApiErrorI18n(c, i18n.MsgGameShopNoItem)
		return
	}
	if err != nil {
		respondGameError(c, riftGame, err)
		return
	}
	view := riftView(session, state.Run, false)
	view["meta"] = riftMetaView(meta)
	common.ApiSuccess(c, gin.H{"session": view})
}

// buyRiftTalent 花道韵把一种天赋升一级。天赋只在开局时带进一轮，已经开始的那一轮不受影响。
func buyRiftTalent(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Id string `json:"id"`
	}
	if decodeGameRequest(c, &req) != nil || req.Id == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var meta *doufa.Meta
	_, err := model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			meta = save.Rift
			return save.Rift.Buy(req.Id)
		})
	})
	switch {
	case errors.Is(err, doufa.ErrCrystals):
		common.ApiErrorI18n(c, i18n.MsgGameDungeonCrystals)
	case errors.Is(err, doufa.ErrMaxed):
		common.ApiErrorI18n(c, i18n.MsgGameDungeonMaxed)
	case errors.Is(err, doufa.ErrUnknown):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case err != nil:
		respondSectError(c, err)
	default:
		common.ApiSuccess(c, gin.H{"meta": riftMetaView(meta)})
	}
}

// buyRiftItem 用游戏币买商店里的道具，放进背包；断网重发同一次购买不会扣两遍币。
func buyRiftItem(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	priceOf := func(id string) (int, bool) {
		item, ok := doufa.ShopItemOf(id)
		return item.Price, ok
	}
	add := func(data string, id string, count int) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error { return save.Rift.AddItem(id, count) })
	}
	data, balance, replayed, ok := buyGameShopItem(c, "wendao_abyss", wendao.SaveKey, priceOf, add, doufa.ErrItemLimit)
	if !ok {
		return
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		respondSectError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": riftMetaView(save.Rift), "balance": balance, "replayed": replayed})
}
