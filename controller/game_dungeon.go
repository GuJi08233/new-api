package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 九幽秘境(原深渊远征)是问道的卡牌肉鸽板块，有三种模式，各自是独立的对局(开一种模式不会作废另一种模式进行中的一局)：
//   - 无尽远征按免费游戏结算，成绩是打通的幕数，按领奖规则发游戏币；
//   - 试炼是固定牌组的关卡，按星级发魂晶，不发游戏币；
//   - 练习用来测试牌与敌人的数值，不结算。
//
// 无尽远征与试炼结束时把魂晶与战绩在结算的同一个事务里记进玩家的局外存档(model.GameSave)。魂晶只能解锁职业与天赋，
// 不能换成游戏币。

const (
	dungeonGame         = wendaoGame
	dungeonTrialGame    = "wendao_trial"
	dungeonPracticeGame = "wendao_practice"
)

// dungeonSessionGames 是各模式的对局存在哪个游戏标识下。领奖次数、最高成绩只算无尽远征(dungeon)。
var dungeonSessionGames = map[string]string{
	dungeon.ModeEndless:  dungeonGame,
	dungeon.ModeTrial:    dungeonTrialGame,
	dungeon.ModePractice: dungeonPracticeGame,
}

// dungeonRunTTL 是一局从开始到结算的最长时间：无尽远征一轮要玩很久，可以分几天玩完。
const dungeonRunTTL = 7 * 24 * 60 * 60

// dungeonSecondsPerStep 是每一步最多算几秒有效游玩时长：出牌前要看敌人意图、算伤害，比解谜的一步想得久。
const dungeonSecondsPerStep = 30

// dungeonState 是保存在对局里的状态：这一局与已经走了几步(用来判断操作是否快过人类)。
type dungeonState struct {
	Steps int          `json:"steps"`
	Run   *dungeon.Run `json:"run"`
}

// abyssInfo 是问道页里秘境板块的规则：局外成长、解锁价格、牌与敌人的图鉴、商店，附上无尽模式的领奖规则与今天的进度。
func abyssInfo(ctx *gameContext) (gin.H, error) {
	rule, _ := ctx.Setting.SkillRuleOf(dungeonGame)
	info, err := skillInfo(ctx, dungeonGame, rule)
	if err != nil {
		return nil, err
	}
	meta, err := loadDungeonMeta(ctx.UserId)
	if err != nil {
		return nil, err
	}
	wendaoPartGate(ctx, info, dungeonGame)
	info["meta"] = dungeonMetaView(meta)
	info["catalog"] = gin.H{
		"class_costs":          dungeon.ClassCosts,
		"talents":              dungeon.Talents,
		"max_abyss":            dungeon.MaxAbyss,
		"content":              dungeon.NewCatalog(),
		"practice_max_stage":   dungeon.PracticeMaxStage,
		"practice_max_deck":    dungeon.PracticeMaxDeck,
		"practice_max_enemies": dungeon.PracticeMaxEnemies,
		"items_per_run":        dungeon.ItemsPerRun,
		"max_item_stack":       dungeon.MaxItemStack,
	}
	ids, cents := make([]string, 0, len(dungeon.ShopItems)), make([]int, 0, len(dungeon.ShopItems))
	for _, item := range dungeon.ShopItems {
		ids, cents = append(ids, item.Id), append(cents, item.Price)
	}
	if info["shop"], err = gameShopInfo(ctx.Setting, ids, cents); err != nil {
		return nil, err
	}
	return info, nil
}

// registerAbyssRoutes 在 /api/game/wendao/abyss 下注册秘境板块的接口。
func registerAbyssRoutes(group *gin.RouterGroup) {
	group.GET("/state", getDungeonRun)
	group.POST("/start", middleware.GamePlayRateLimit("dungeon-start"), startDungeonRun)
	group.POST("/action", middleware.GameActionRateLimit("dungeon-action"), actDungeonRun)
	group.POST("/unlock", middleware.GamePlayRateLimit("dungeon-unlock"), unlockDungeon)
	group.POST("/shop/buy", middleware.GamePlayRateLimit("dungeon-shop"), buyDungeonItem)
}

func loadDungeonMeta(userId int) (*dungeon.Meta, error) {
	data, err := model.GetGameSave(userId, wendao.SaveKey)
	if err != nil {
		return nil, err
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		return nil, err
	}
	return save.Abyss, nil
}

// updateAbyssSave 改写问道存档里的秘境局外成长。
func updateAbyssSave(data string, edit func(meta *dungeon.Meta) error) (string, error) {
	return wendao.UpdateSave(data, func(save *wendao.Save) error { return edit(save.Abyss) })
}

// dungeonMetaView 是局外存档在页面上的样子，附上每个职业现在能选的最高深渊等级。
func dungeonMetaView(meta *dungeon.Meta) gin.H {
	maxAbyss := map[string]int{}
	for _, class := range dungeon.Classes {
		maxAbyss[class] = meta.MaxAbyss(class)
	}
	return gin.H{
		"crystals":  meta.Crystals,
		"earned":    meta.Earned,
		"runs":      meta.Runs,
		"wins":      meta.Wins,
		"classes":   meta.Classes,
		"talents":   meta.Talents,
		"records":   meta.Records,
		"trials":    meta.Trials,
		"items":     meta.Items,
		"max_abyss": maxAbyss,
	}
}

func dungeonRunView(session *model.GameSession, state *dungeonState) gin.H {
	return gin.H{"session_id": session.Id, "version": session.Version, "status": session.Status, "run": state.Run.View()}
}

func parseDungeonState(session *model.GameSession) (*dungeonState, error) {
	state := &dungeonState{}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil {
		return nil, err
	}
	if state.Run == nil {
		return nil, errors.New("dungeon run state is empty")
	}
	return state, nil
}

// dungeonModeGame 返回模式(不填为无尽远征)的对局存在哪个游戏标识下；模式不认识时写好错误响应并返回空字符串。
func dungeonModeGame(c *gin.Context, mode string) string {
	if mode == "" {
		mode = dungeon.ModeEndless
	}
	game, ok := dungeonSessionGames[mode]
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	}
	return game
}

func getDungeonRun(c *gin.Context) {
	if !requireGameEnabled(c, dungeonGame) {
		return
	}
	game := dungeonModeGame(c, c.Query("mode"))
	if game == "" {
		return
	}
	var session *model.GameSession
	var err error
	if requested := c.Query("session_id"); requested != "" {
		id, parseErr := strconv.Atoi(requested)
		if parseErr != nil || id <= 0 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		session, err = model.GetUserGameSession(c.GetInt("id"), game, id)
	} else {
		session, err = model.GetPlayingGameSession(c.GetInt("id"), game)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || (session.Status == model.GameSessionStatusPlaying && common.GetTimestamp()-session.CreatedAt > dungeonRunTTL) {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	state, err := parseDungeonState(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view := dungeonRunView(session, state)
	if state.Run.Version != dungeon.StateVersion {
		view["outdated"] = true
	}
	if session.Status == model.GameSessionStatusFinished {
		rule, _ := operation_setting.GetGameSetting().SkillRuleOf(game)
		result, err := skillSettledView(c, session, rule)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		view["result"] = result
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

func startDungeonRun(c *gin.Context) {
	if !requireGameEnabled(c, dungeonGame) {
		return
	}
	var req struct {
		Mode     string                 `json:"mode"`
		Class    string                 `json:"class"`
		Abyss    int                    `json:"abyss"`
		Trial    string                 `json:"trial"`
		Practice *dungeon.PracticeSetup `json:"practice"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	game := dungeonModeGame(c, req.Mode)
	if game == "" {
		return
	}
	userId := c.GetInt("id")
	meta, err := loadDungeonMeta(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var newRun func(seed uint64) (*dungeon.Run, error)
	switch game {
	case dungeonGame:
		// 开新的一轮会作废进行中的那一轮，进度和魂晶都会丢：必须先放弃(结算)或者打完，过期的不算。
		playing, err := model.GetPlayingGameSession(userId, dungeonGame)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if playing != nil && common.GetTimestamp()-playing.CreatedAt <= dungeonRunTTL {
			common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
			return
		}
		options, err := meta.Options(req.Class, req.Abyss)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgGameDungeonLocked)
			return
		}
		newRun = func(seed uint64) (*dungeon.Run, error) { return dungeon.NewRun(seed, options) }
	case dungeonTrialGame:
		if !meta.TrialUnlocked(req.Trial) {
			common.ApiErrorI18n(c, i18n.MsgGameDungeonLocked)
			return
		}
		newRun = func(seed uint64) (*dungeon.Run, error) { return dungeon.NewTrial(seed, req.Trial) }
	default:
		if req.Practice == nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		if _, err := dungeon.NewPractice(0, *req.Practice); err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		newRun = func(seed uint64) (*dungeon.Run, error) { return dungeon.NewPractice(seed, *req.Practice) }
	}
	var state *dungeonState
	session := startSkillSession(c, game, func(seed uint32) (string, error) {
		run, err := newRun(uint64(seed))
		if err != nil {
			return "", err
		}
		state = &dungeonState{Run: run}
		data, err := common.Marshal(state)
		return string(data), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, dungeonRunView(session, state))
}

func actDungeonRun(c *gin.Context) {
	if !requireGameEnabled(c, dungeonGame) {
		return
	}
	var req struct {
		Mode      string `json:"mode"`
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		Node      *int   `json:"node"`
		Card      *int   `json:"card"`
		Target    *int   `json:"target"`
		Choice    *int   `json:"choice"`
		Potion    *int   `json:"potion"`
		Item      string `json:"item"`
	}
	if decodeGameRequest(c, &req) != nil || req.Action == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	game := dungeonModeGame(c, req.Mode)
	if game == "" {
		return
	}
	// 试炼与练习不发游戏币，领奖规则为空。
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(game)
	session := loadSkillSession(c, game, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	state, err := parseDungeonState(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	run := state.Run
	// 练习不结算，也就没有放弃：离开页面就行，下次开练习时作废。
	if req.Action == "abandon" && game == dungeonPracticeGame {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Action != "abandon" {
		if run.Version != dungeon.StateVersion {
			common.ApiErrorI18n(c, i18n.MsgGameDungeonOutdated)
			return
		}
		orNone := func(v *int) int {
			if v == nil {
				return -1
			}
			return *v
		}
		if req.Action == "item" {
			// 用背包里的道具：道具从存档里拿走与这一步的新局面在同一个事务里保存，道具不会白扣也不会白用。
			useDungeonItem(c, game, session, state, req.Item)
			return
		}
		action := dungeon.Action{Type: req.Action, Node: orNone(req.Node), Card: orNone(req.Card), Target: orNone(req.Target),
			Choice: orNone(req.Choice), Potion: orNone(req.Potion)}
		if err := run.Apply(action); err != nil {
			common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
			return
		}
		state.Steps++
		elapsed := max(int64(0), common.GetTimestamp()-session.CreatedAt)
		if int64(state.Steps) > (elapsed+2)*8 {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view := dungeonRunView(session, state)
	if !run.Over() && req.Action != "abandon" {
		if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
			respondGameError(c, game, err)
			return
		}
		view["version"] = session.Version
		common.ApiSuccess(c, view)
		return
	}

	// 一局结束(倒下、试炼过关或放弃)：按成绩结算，并在同一个事务里把魂晶与战绩记进存档。记录先按结算前读到的存档算好写进
	// 明细，事务里再按锁住的存档重算：同一玩家同一模式同时只有一局，两次结果相同。
	meta, err := loadDungeonMeta(session.UserId)
	if err != nil {
		respondGameFinishRetry(c)
		return
	}
	record := func(m *dungeon.Meta) dungeon.RunRecord {
		if game == dungeonTrialGame {
			return m.RecordTrial(run)
		}
		return m.Record(run)
	}
	preview := record(meta)
	save := &model.GameSaveUpdate{Game: wendao.SaveKey, Update: func(data string) (string, error) {
		return updateAbyssSave(data, func(current *dungeon.Meta) error {
			record(current)
			return nil
		})
	}}
	outcome := skillOutcome{
		Score:   run.Score(),
		Seconds: min(session.PlayedSeconds(common.GetTimestamp()), state.Steps*dungeonSecondsPerStep),
		State:   string(data),
		Detail: gin.H{"class": run.Class, "abyss": run.Abyss, "floor": run.Floor, "acts": run.Bosses,
			"abandoned": req.Action == "abandon", "crystals": preview.Crystals, "bonus": preview.Bonus},
		Save: save,
	}
	name := "九幽秘境"
	if game == dungeonTrialGame {
		name = "九幽秘境试炼"
		outcome.Score = preview.Stars
		outcome.Detail = gin.H{"trial": run.Trial, "stars": preview.Stars, "turns": run.Turns,
			"abandoned": req.Action == "abandon", "crystals": preview.Crystals}
	}
	result := settleSkillSession(c, name, rule, session, outcome)
	if result == nil {
		return
	}
	view["status"], view["result"], view["version"] = model.GameSessionStatusFinished, result, session.Version+1
	common.ApiSuccess(c, view)
}

func unlockDungeon(c *gin.Context) {
	if !requireGameEnabled(c, dungeonGame) {
		return
	}
	var req struct {
		Item string `json:"item"`
	}
	if decodeGameRequest(c, &req) != nil || req.Item == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var meta *dungeon.Meta
	_, err := model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return updateAbyssSave(data, func(current *dungeon.Meta) error {
			meta = current
			return current.Buy(req.Item)
		})
	})
	switch {
	case errors.Is(err, dungeon.ErrCrystals):
		common.ApiErrorI18n(c, i18n.MsgGameDungeonCrystals)
		return
	case errors.Is(err, dungeon.ErrMaxed):
		common.ApiErrorI18n(c, i18n.MsgGameDungeonMaxed)
		return
	case errors.Is(err, dungeon.ErrAction):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": dungeonMetaView(meta)})
}

// useDungeonItem 在进行中的一轮里用掉背包里的一件道具。道具不会让一轮结束(护命符只是挂在身上)，所以只保存新局面，不结算。
func useDungeonItem(c *gin.Context, game string, session *model.GameSession, state *dungeonState, item string) {
	meta, err := loadDungeonMeta(session.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if meta.Items[item] < 1 {
		common.ApiErrorI18n(c, i18n.MsgGameShopNoItem)
		return
	}
	switch err := state.Run.UseItem(item); {
	case errors.Is(err, dungeon.ErrItemLimit):
		common.ApiErrorI18n(c, i18n.MsgGameShopItemLimit)
		return
	case err != nil:
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	err = model.UpdateGameSessionAndSave(session, string(data), "", wendao.SaveKey, func(save string) (string, error) {
		return updateAbyssSave(save, func(current *dungeon.Meta) error {
			meta = current
			return current.TakeItem(item)
		})
	})
	if errors.Is(err, dungeon.ErrNoItem) {
		common.ApiErrorI18n(c, i18n.MsgGameShopNoItem)
		return
	}
	if err != nil {
		respondGameError(c, game, err)
		return
	}
	view := dungeonRunView(session, state)
	view["meta"] = dungeonMetaView(meta)
	common.ApiSuccess(c, view)
}

func buyDungeonItem(c *gin.Context) {
	if !requireGameEnabled(c, dungeonGame) {
		return
	}
	priceOf := func(id string) (int, bool) {
		item, ok := dungeon.ShopItemOf(id)
		return item.Price, ok
	}
	add := func(data string, id string, count int) (string, error) {
		return updateAbyssSave(data, func(meta *dungeon.Meta) error { return meta.AddItem(id, count) })
	}
	data, balance, ok := buyGameShopItem(c, "wendao_abyss", wendao.SaveKey, priceOf, add, dungeon.ErrItemLimit)
	if !ok {
		return
	}
	save, err := wendao.ParseSave(data)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"meta": dungeonMetaView(save.Abyss), "balance": balance})
}
