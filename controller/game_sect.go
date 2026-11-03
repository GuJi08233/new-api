package controller

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 宗门是问道的经营板块：宗门存在玩家的局外存档(model.GameSave)里，产出与修炼由服务端按时间结算，所有经营
// 操作都锁住存档改写，不发游戏币。游戏币只从「宗门大比」来：大比是一局免费游戏，成绩是连胜场数，按领奖规则发放；大比
// 给宗门的声望、灵石与没用完的回春丹在结算的同一个事务里写回存档。

// sectTournamentGame 是宗门大比的对局与领奖规则用的游戏标识：大比有自己的开关与领奖规则，关掉它不影响问道的其他玩法。
const sectTournamentGame = "wendao_tournament"

// sectSecondsPerStep 是大比每一步最多算几秒有效游玩时长：要看对手的下一招、算伤害与五行。
const sectSecondsPerStep = 15

// sectTournamentState 是保存在对局里的大比状态与已经走了几步(用来判断操作是否快过人类)。
type sectTournamentState struct {
	Steps      int                `json:"steps"`
	Tournament *wendao.Tournament `json:"tournament"`
	// Defender 是论剑的守擂方，大比为 0。
	Defender int `json:"defender,omitempty"`
}

// sectInfo 是问道页里宗门板块的规则：大比、离线收益上限与宗门商店，附上宗门大比的领奖规则与今天的进度。
func sectInfo(ctx *gameContext) (gin.H, error) {
	rule, _ := ctx.Setting.SkillRuleOf(sectTournamentGame)
	info, err := skillInfo(ctx, sectTournamentGame, rule)
	if err != nil {
		return nil, err
	}
	info["tournament"] = gin.H{
		"rounds":            wendao.TournamentRounds,
		"team_size":         wendao.TeamSize,
		"pouch_pills":       wendao.PouchPills,
		"turns":             wendao.DuelTurns,
		"daily_reward_runs": wendao.DailyRewardRuns,
	}
	wendaoPartGate(ctx, info, sectTournamentGame)
	info["offline_cap"] = wendao.OfflineCap
	info["max_item_stack"] = wendao.MaxItemStack
	info["path"] = gin.H{
		"sects": wendao.NpcSects, "rank_merit": wendao.RankMerit, "rank_realm": wendao.RankRealm,
		"rank_stipend": wendao.RankStipend, "found_realm": wendao.FoundRealm, "found_cost": wendao.FoundCost,
		"rejoin_wait": wendao.PathRejoinWait, "daily_merit": wendao.PathDailyMerit, "donate_rate": wendao.DonateRate,
		"perks": wendao.PerkValues,
	}
	ids, cents := make([]string, 0, len(wendao.ShopItems)), make([]int, 0, len(wendao.ShopItems))
	for _, item := range wendao.ShopItems {
		ids, cents = append(ids, item.Id), append(cents, item.Price)
	}
	if info["shop"], err = gameShopInfo(ctx.Setting, ids, cents); err != nil {
		return nil, err
	}
	return info, nil
}

// registerSectRoutes 在 /api/game/wendao/sect 下注册宗门板块的接口。
func registerSectRoutes(group *gin.RouterGroup) {
	group.POST("/action", middleware.GameActionRateLimit("sect-action"), actSect)
	group.POST("/path", middleware.GameActionRateLimit("sect-path"), actSectPath)
	group.GET("/outpost", getSectOutpost)
	group.POST("/outpost", middleware.GameActionRateLimit("sect-outpost"), actSectOutpost)
	group.GET("/lineage", getSectLineage)
	group.POST("/lineage", middleware.GameActionRateLimit("sect-lineage"), actSectLineage)
	group.GET("/tournament", getSectTournament)
	group.POST("/tournament/start", middleware.GamePlayRateLimit("sect-tournament-start"), startSectTournament)
	group.POST("/tournament/action", middleware.GameActionRateLimit("sect-tournament-action"), actSectTournament)
	group.POST("/shop/buy", middleware.GamePlayRateLimit("sect-shop"), buySectItem)
	group.GET("/arena", getSectArena)
	group.POST("/arena/defense", middleware.GamePlayRateLimit("sect-arena-defense"), setSectArenaDefense)
	group.GET("/arena/battle", getSectArenaBattle)
	group.POST("/arena/start", middleware.GamePlayRateLimit("sect-arena-start"), startSectArena)
	group.POST("/arena/action", middleware.GameActionRateLimit("sect-arena-action"), actSectArena)
	group.GET("/alliance", getSectAlliance)
	group.POST("/alliance/create", middleware.GamePlayRateLimit("sect-alliance"), createSectAlliance)
	group.POST("/alliance/join", middleware.GamePlayRateLimit("sect-alliance"), joinSectAlliance)
	group.POST("/alliance/leave", middleware.GamePlayRateLimit("sect-alliance"), leaveSectAlliance)
	group.POST("/alliance/donate", middleware.GameActionRateLimit("sect-alliance-donate"), donateSectAlliance)
	group.POST("/alliance/upgrade", middleware.GamePlayRateLimit("sect-alliance"), upgradeSectAlliance)
	group.POST("/alliance/notice", middleware.GamePlayRateLimit("sect-alliance"), setSectAllianceNotice)
	group.POST("/alliance/attack", middleware.GamePlayRateLimit("sect-alliance-attack"), attackSectAlliance)
	group.POST("/alliance/claim", middleware.GamePlayRateLimit("sect-alliance"), claimSectAlliance)
	registerSectGardenRoutes(group.Group("/garden"))
	registerSectMarketRoutes(group.Group("/market"))
}

// newSectSeed 是新宗门随机数的种子：两次 32 位安全随机数拼成 64 位。
func newSectSeed() (uint64, error) {
	high, err := newGameSeed()
	if err != nil {
		return 0, err
	}
	low, err := newGameSeed()
	return uint64(high)<<32 | uint64(low), err
}

// respondSectError 把规则包的错误写成响应。
func respondSectError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, wendao.ErrCost):
		common.ApiErrorI18n(c, i18n.MsgGameSectResources)
	case errors.Is(err, wendao.ErrUnavailable):
		common.ApiErrorI18n(c, i18n.MsgGameSectUnavailable)
	case errors.Is(err, wendao.ErrAction):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, wendao.ErrNoItem):
		common.ApiErrorI18n(c, i18n.MsgGameShopNoItem)
	case errors.Is(err, wendao.ErrItemLimit):
		common.ApiErrorI18n(c, i18n.MsgGameShopItemLimit)
	case errors.Is(err, wendao.ErrNewerSave):
		common.ApiErrorI18n(c, i18n.MsgGameWendaoNewerSave)
	case errors.Is(err, wendao.ErrRequestReused):
		common.ApiErrorI18n(c, i18n.MsgGameWendaoRequestReused)
	case errors.Is(err, wendao.ErrBattleMoved):
		common.ApiErrorI18n(c, i18n.MsgGameWendaoBattleMoved)
	default:
		common.ApiError(c, err)
	}
}

// loadWendaoSave 读出玩家的问道存档，只读不写。
func loadWendaoSave(userId int) (*wendao.Save, error) {
	data, err := model.GetGameSave(userId, wendao.SaveKey)
	if err != nil {
		return nil, err
	}
	return wendao.ParseSave(data)
}

// ensureStorehouse 在还没有储物处时给主角一个(散修的储物处：没有主殿与弟子)。开宗立派另走 path 接口。
func ensureStorehouse(save *wendao.Save, now int64) error {
	if save.Sect != nil {
		return nil
	}
	seed, err := newSectSeed()
	if err != nil {
		return err
	}
	save.Sect = wendao.NewStorehouse(seed, now)
	return nil
}

// actSect 执行一次经营操作：锁住存档，先结算到现在，再执行操作。还没有宗门时先开宗立派(任何操作都会建好宗门，页面打开时
// 发一次 sync)。
func actSect(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req wendao.Action
	if decodeGameRequest(c, &req) != nil || req.Type == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	requestId := req.RequestId
	req.RequestId = ""
	sum, err := wendao.RequestSum(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	// 仙盟等级给产出加成：每次操作前按仙盟表刷新，结算这段时间的产出时用上。
	allianceLevel, err := model.GetSectAllianceLevel(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var evs []wendao.Ev
	var current *wendao.Save
	_, err = model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(requestId, sum, now); err != nil {
				return err
			}
			if err := ensureStorehouse(save, now); err != nil {
				return err
			}
			// 恢复可见时宗门页和顶部会同时心跳：先拿到锁的一方保存同一份真实回流结算。
			// 在 Resume 前刷新仙盟等级，沿用宗门同步按当前仙盟加成结算的规则。
			if req.Type == "sync" {
				save.Sect.AllianceLevel = allianceLevel
				save.Resume(now)
			}
			// 主角的闭关修为跟宗门一起结算，只经营宗门不会让主角的离线修为超过上限白白浪费。
			save.TickHero(now)
			rollWendaoDaily(save)
			s := save.Sect
			s.AllianceLevel = allianceLevel
			var err error
			if evs, err = s.Apply(req, now); err != nil {
				return err
			}
			// 宗门弟子领一件每日功课，宗门多记一笔贡献。
			if req.Type == "daily_claim" {
				evs = append(evs, save.GrantMerit(wendao.PathDailyMerit)...)
			}
			current = save
			return nil
		})
	})
	if errors.Is(err, wendao.ErrReplayed) {
		// 这一步上次已经做完了，只是页面没收到回应：交回现在的样子，不再做一遍。
		save, err := loadWendaoSave(c.GetInt("id"))
		if err == nil && save.Sect == nil {
			err = wendao.ErrUnavailable
		}
		if err != nil {
			respondSectError(c, err)
			return
		}
		respondSectManagement(c, save, now, nil, true)
		return
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	respondSectManagement(c, current, now, evs, false)
}

func sectTournamentView(session *model.GameSession, state *sectTournamentState) gin.H {
	return gin.H{"session_id": session.Id, "version": session.Version, "status": session.Status,
		"tournament": state.Tournament.View(), "defender": state.Defender}
}

func parseSectTournament(session *model.GameSession) (*sectTournamentState, error) {
	state := &sectTournamentState{}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil {
		return nil, err
	}
	if state.Tournament == nil {
		return nil, errors.New("sect tournament state is empty")
	}
	return state, nil
}

// sectDuelRule 是一种斗法的领奖规则：大比按管理员设的规则发游戏币，论剑不发(规则为空)。
func sectDuelRule(game string) operation_setting.SkillRule {
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(game)
	return rule
}

func getSectTournament(c *gin.Context) { getSectDuel(c, sectTournamentGame) }

// getSectDuel 读出进行中(或按 session_id 指定)的一场大比或论剑，game 是对局存在哪个游戏标识下。
func getSectDuel(c *gin.Context, game string) {
	if !requireGameEnabled(c, wendaoGame) {
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
	if session == nil || (session.Status == model.GameSessionStatusPlaying && common.GetTimestamp()-session.CreatedAt > skillSessionTTL) {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	state, err := parseSectTournament(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view := sectTournamentView(session, state)
	if session.Status == model.GameSessionStatusFinished {
		result, err := skillSettledView(c, session, sectDuelRule(game))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		view["result"] = result
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

// startSectTournament 带选好的弟子参加大比。
func startSectTournament(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) || !requireGameEnabled(c, sectTournamentGame) {
		return
	}
	var req struct {
		Team []int `json:"team"`
	}
	if decodeGameRequest(c, &req) != nil || len(req.Team) == 0 || len(req.Team) > wendao.TeamSize {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	startSectDuel(c, sectTournamentGame, 0, func(seed uint64, s *wendao.Sect, now int64) (*wendao.Tournament, error) {
		return wendao.NewTournament(seed, s, req.Team, now)
	})
}

// startSectDuel 开一场大比或论剑：build 按宗门存档建出斗法(seed 为 0 时只检查报名弟子)。校验队伍、扣丹与创建会话在同一
// 存档锁事务里，派驻下院不会穿插在开局中间；开局失败不扣丹，已开局超时的丹药不退。defender 是论剑守擂方(大比为 0)。
func startSectDuel(c *gin.Context, game string, defender int, build func(seed uint64, s *wendao.Sect, now int64) (*wendao.Tournament, error)) {
	userId := c.GetInt("id")
	// 还有一场没打完时不能开新的：新的一局会作废旧的，旧的带走的回春丹就没了。
	playing, err := model.GetPlayingGameSession(userId, game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if playing != nil && common.GetTimestamp()-playing.CreatedAt <= skillSessionTTL {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	// 先按当前存档检查报名的弟子，让「弟子受伤、外出」这类问题得到明确的提示，而不是笼统的开局失败。
	data, err := model.GetGameSave(userId, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	s, err := wendao.ParseSect(data)
	if err != nil {
		respondSectError(c, err)
		return
	}
	if s == nil {
		common.ApiErrorI18n(c, i18n.MsgGameSectUnavailable)
		return
	}
	now := common.GetTimestamp()
	if _, err := build(0, s, now); err != nil {
		respondSectError(c, err)
		return
	}
	if !passGamePlayTurnstile(c, operation_setting.GetGameSetting(), game, userId) {
		return
	}
	seed, err := newGameSeed()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameStartFailed)
		return
	}
	var state *sectTournamentState
	session, err := model.CreateGameSessionAndSave(userId, game, seed, common.GetTimestamp()-skillSessionTTL, wendao.SaveKey, func(data string) (string, string, error) {
		saveData, err := updateSectSave(data, func(s *wendao.Sect) error {
			now := common.GetTimestamp()
			s.Tick(now)
			tournament, err := build(uint64(seed), s, now)
			if err != nil {
				return err
			}
			s.Pills[wendao.PillHeal] -= tournament.Taken
			state = &sectTournamentState{Tournament: tournament, Defender: defender}
			return nil
		})
		if err != nil {
			return "", "", err
		}
		encoded, err := common.Marshal(state)
		return string(encoded), saveData, err
	})
	if errors.Is(err, model.ErrGameRoundOpen) {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	common.ApiSuccess(c, sectTournamentView(session, state))
}

func actSectTournament(c *gin.Context) { actSectDuel(c, sectTournamentGame) }

// actSectDuel 在一场大比或论剑里走一步(选人上场、出招或认输)。结束时按连胜场数结算，并在同一个事务里把声望、灵石与没用完的
// 回春丹记进宗门；论剑另外按 Elo 改双方积分。
func actSectDuel(c *gin.Context, game string) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		Index     int    `json:"index"`
	}
	if decodeGameRequest(c, &req) != nil || req.Action == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule := sectDuelRule(game)
	session := loadSkillSession(c, game, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	state, err := parseSectTournament(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tournament := state.Tournament
	abandon := req.Action == "abandon"
	if !abandon {
		if err := tournament.Play(req.Action, req.Index); err != nil {
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
	view := sectTournamentView(session, state)
	if !tournament.Over() && !abandon {
		if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
			respondGameError(c, game, err)
			return
		}
		view["version"] = session.Version
		common.ApiSuccess(c, view)
		return
	}

	// 斗法结束(输了一场、全胜或认输)：奖励先按结算前读到的存档算好写进明细，事务里再按锁住的存档记一遍：同一玩家同一种斗法
	// 同时只有一场，两次结果相同。
	arena := game == sectArenaGame
	record := func(s *wendao.Sect, day string, now int64) wendao.Cost {
		if arena {
			return s.RecordArena(tournament, day, now)
		}
		return s.RecordTournament(tournament, day, now)
	}
	now := common.GetTimestamp()
	day := gameCenterDay()
	saved, err := model.GetGameSave(session.UserId, wendao.SaveKey)
	if err != nil {
		respondGameFinishRetry(c)
		return
	}
	preview, err := wendao.ParseSect(saved)
	if err != nil || preview == nil {
		respondGameFinishRetry(c)
		return
	}
	gained := record(preview, day, now)
	save := &model.GameSaveUpdate{Game: wendao.SaveKey, Update: func(data string) (string, error) {
		return updateSectSave(data, func(s *wendao.Sect) error {
			record(s, day, now)
			return nil
		})
	}}
	outcome := skillOutcome{
		Score:   tournament.Score(),
		Seconds: min(session.PlayedSeconds(now), state.Steps*sectSecondsPerStep),
		State:   string(data),
		Detail: gin.H{"wins": tournament.Wins, "champion": tournament.Champion(), "abandoned": abandon,
			"stone": gained.Stone, "rep": gained.Rep, "pills": tournament.Pills, "defender": state.Defender},
		Save: save,
	}
	name := "问道宗门大比"
	if arena {
		name = "问道宗门论剑"
	}
	result := settleSkillSession(c, name, rule, session, outcome)
	if result == nil {
		return
	}
	if arena {
		// 积分在结算之后另记：同一局只记一次(按对局编号去重)。结算成功而积分没记上时只是少一次排名变动，资源与丹药不受影响。
		battle, err := model.RecordSectArenaBattle(session.Id, session.UserId, state.Defender, tournament.Champion(), tournament.Wins)
		if err != nil && !errors.Is(err, model.ErrSectArenaBattleRecorded) {
			common.SysError(fmt.Sprintf("failed to record sect arena battle %d: %v", session.Id, err))
		}
		if battle != nil {
			view["battle"] = battle
		}
	}
	view["status"], view["result"], view["version"] = model.GameSessionStatusFinished, result, session.Version+1
	common.ApiSuccess(c, view)
}

// buySectItem 用游戏币买宗门道具：丹药直接进丹房，符箓进背包。还没有宗门时先开宗立派。
func buySectItem(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	priceOf := func(id string) (int, bool) {
		item, ok := wendao.ShopItemOf(id)
		return item.Price, ok
	}
	now := common.GetTimestamp()
	add := func(data string, id string, count int) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := ensureStorehouse(save, now); err != nil {
				return err
			}
			return save.Sect.AddItem(id, count)
		})
	}
	data, balance, replayed, ok := buyGameShopItem(c, "wendao_sect", wendao.SaveKey, priceOf, add, wendao.ErrItemLimit)
	if !ok {
		return
	}
	s, err := wendao.ParseSect(data)
	if err == nil && s == nil {
		err = wendao.ErrUnavailable
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sect": s.View(now), "events": []wendao.Ev{}, "balance": balance, "replayed": replayed})
}
