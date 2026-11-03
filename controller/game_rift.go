package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 秘境斗法：问道秘境板块里即时斗法的一轮(规则见 pkg/doufa 的 Run)。一轮的状态存在对局里，战斗房由浏览器打完提交
// 操作记录，服务端按房间的种子与开局重放核对，只认重放出来的结果。现在不发游戏币(领奖规则为空)，只记成绩。
const riftGame = "wendao_rift"

// riftGraceSeconds 是核对打一间房用时的余量：提交时间不能早于"帧数 ÷ 60"的八成减去这几秒(网络与页面切换的误差)。
const riftGraceSeconds = 2

type riftState struct {
	Run *doufa.Run `json:"run"`
}

func registerRiftRoutes(group *gin.RouterGroup) {
	group.GET("/state", getRiftRun)
	group.POST("/start", middleware.GamePlayRateLimit("rift-start"), startRiftRun)
	group.POST("/act", middleware.GameActionRateLimit("rift-act"), actRiftRun)
}

func parseRiftState(session *model.GameSession) (*riftState, error) {
	state := &riftState{}
	if err := common.UnmarshalJsonStr(session.State, state); err != nil {
		return nil, err
	}
	if state.Run == nil {
		return nil, errors.New("rift run state is empty")
	}
	return state, nil
}

func riftView(session *model.GameSession, run *doufa.Run) gin.H {
	view := gin.H{"session_id": session.Id, "version": session.Version, "status": session.Status, "run": run,
		"max_hp": run.MaxHP(), "score": run.Score()}
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
	if session == nil || common.GetTimestamp()-session.CreatedAt > dungeonRunTTL {
		common.ApiSuccess(c, gin.H{"session": nil, "best": best})
		return
	}
	state, err := parseRiftState(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": riftView(session, state.Run), "best": best})
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
	if _, err := doufa.NewRun(1, req.Class); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 开新的一轮要先打完或放弃进行中的那一轮，过期的不算。
	playing, err := model.GetPlayingGameSession(c.GetInt("id"), riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if playing != nil && common.GetTimestamp()-playing.CreatedAt <= dungeonRunTTL {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	var run *doufa.Run
	session := startSkillSession(c, riftGame, func(seed uint32) (string, error) {
		var err error
		if run, err = doufa.NewRun(seed, req.Class); err != nil {
			return "", err
		}
		data, err := common.Marshal(riftState{Run: run})
		return string(data), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, gin.H{"session": riftView(session, run)})
}

// actRiftRun 走一步：enter 进第 choice 间房，pick 挑第 choice 样(法宝换第 slot 件)，clear 提交打完的房间(inputs 是
// 操作记录)，abandon 放弃。version 要与对局一致，另一个页面先走了这一步就拒绝。一轮结束(倒下或放弃)时按走到多深结算。
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
	}
	if decodeGameRequest(c, &req) != nil || req.Action == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(riftGame)
	session := loadSkillSession(c, riftGame, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	state, err := parseRiftState(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	run := state.Run
	if run.V != doufa.RunStateVersion && req.Action != "abandon" {
		common.ApiErrorI18n(c, i18n.MsgGameDungeonOutdated)
		return
	}
	now := common.GetTimestamp()
	switch req.Action {
	case "enter":
		err = run.Enter(req.Choice, now)
	case "pick":
		err = run.Pick(req.Choice, req.Slot)
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
		err = run.Clear(fight)
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
	if !run.Over {
		if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
			respondGameError(c, riftGame, err)
			return
		}
		common.ApiSuccess(c, gin.H{"session": riftView(session, run)})
		return
	}
	outcome := skillOutcome{
		Score:   run.Score(),
		Seconds: min(session.PlayedSeconds(now), run.Ticks/doufa.FPS+60),
		State:   string(data),
		Detail: gin.H{"class": run.Class, "floor": run.Floor, "step": run.Step, "cleared": run.Cleared, "kills": run.Kills,
			"stones": run.Stones, "abandoned": req.Action == "abandon"},
	}
	result := settleSkillSession(c, "九幽秘境斗法", rule, session, outcome)
	if result == nil {
		return
	}
	view := riftView(session, run)
	view["status"], view["result"], view["version"] = model.GameSessionStatusFinished, result, session.Version+1
	common.ApiSuccess(c, gin.H{"session": view})
}
