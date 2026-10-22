package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/hanoi"
	"github.com/QuantumNous/new-api/pkg/lightsout"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// logicPuzzleState 只保存难度与合法操作，局面从会话种子重建；撤销和重置不相信客户端传来的盘面。
type logicPuzzleState struct {
	Difficulty int   `json:"difficulty"`
	Inputs     []int `json:"inputs"`
}

func init() {
	for _, key := range []string{"lightsout", "hanoi"} {
		registerGame(&gameDefinition{Key: key, Kind: operation_setting.GameKindSkill,
			Info: func(ctx *gameContext) (gin.H, error) {
				rule, _ := ctx.Setting.SkillRuleOf(key)
				return skillInfo(ctx, key, rule)
			},
			Routes: func(group *gin.RouterGroup) {
				group.GET("/state", func(c *gin.Context) { getLogicPuzzle(c, key) })
				group.POST("/start", middleware.GamePlayRateLimit(key+"-start"), func(c *gin.Context) { startLogicPuzzle(c, key) })
				group.POST("/action", middleware.GameActionRateLimit(key+"-action"), func(c *gin.Context) { actLogicPuzzle(c, key) })
			},
		})
	}
}

// logicPuzzleView 用规则引擎重放服务端保存的操作，所有盘面信息都公开，两种玩法共用存档、版本和领奖流程。
func logicPuzzleView(key string, session *model.GameSession, state logicPuzzleState) (gin.H, error) {
	view := gin.H{"session_id": session.Id, "version": session.Version, "difficulty": state.Difficulty,
		"moves": len(state.Inputs), "status": session.Status, "won": false}
	switch key {
	case "lightsout":
		game, err := lightsout.New(state.Difficulty, uint64(session.Seed))
		if err != nil || len(state.Inputs) > lightsout.MaxMoves {
			return nil, lightsout.ErrMove
		}
		for _, cell := range state.Inputs {
			if err := game.Move(cell); err != nil {
				return nil, err
			}
		}
		view["board"], view["size"], view["won"], view["max_moves"] = game.Board, game.Size, game.Solved(), lightsout.MaxMoves
	case "hanoi":
		game, err := hanoi.New(state.Difficulty)
		if err != nil || len(state.Inputs) > hanoi.MaxMoves {
			return nil, hanoi.ErrMove
		}
		for _, move := range state.Inputs {
			if move < 0 || move > 8 {
				return nil, hanoi.ErrMove
			}
			if err := game.Move(move/3, move%3); err != nil {
				return nil, err
			}
		}
		view["towers"], view["disks"], view["won"] = game.Towers, game.Disks, game.Solved()
		view["minimum_moves"], view["max_moves"] = game.MinimumMoves(), hanoi.MaxMoves
	default:
		return nil, errors.New("unknown logic puzzle")
	}
	return view, nil
}

func startLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
		return
	}
	var req struct {
		Difficulty int `json:"difficulty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Difficulty < 1 || req.Difficulty > 3 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	state := logicPuzzleState{Difficulty: req.Difficulty, Inputs: []int{}}
	session := startSkillSession(c, key, func(uint32) (string, error) {
		data, err := common.Marshal(state)
		return string(data), err
	})
	if session == nil {
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func getLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
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
		session, err = model.GetUserGameSession(c.GetInt("id"), key, id)
	} else {
		session, err = model.GetPlayingGameSession(c.GetInt("id"), key)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var state logicPuzzleState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		common.ApiError(c, err)
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session.Status == model.GameSessionStatusFinished {
		rule, _ := operation_setting.GetGameSetting().SkillRuleOf(key)
		result, err := skillSettledView(c, session, rule)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		view["result"] = result
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

func actLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		Cell      *int   `json:"cell"`
		From      *int   `json:"from"`
		To        *int   `json:"to"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(key)
	session := loadSkillSession(c, key, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	var state logicPuzzleState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		common.ApiError(c, err)
		return
	}
	switch req.Action {
	case "move":
		if key == "lightsout" {
			if req.Cell == nil {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.Cell)
		} else {
			if req.From == nil || req.To == nil || *req.From < 0 || *req.From > 2 || *req.To < 0 || *req.To > 2 {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.From*3+*req.To)
		}
	case "undo":
		if len(state.Inputs) == 0 {
			common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
			return
		}
		state.Inputs = state.Inputs[:len(state.Inputs)-1]
	case "reset":
		state.Inputs = []int{}
	case "finish":
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	elapsed := max(int64(0), common.GetTimestamp()-session.CreatedAt)
	if int64(len(state.Inputs)) > (elapsed+2)*8 {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	won := view["won"].(bool)
	if won || req.Action == "finish" {
		score, seconds := 0, session.PlaySeconds
		if won {
			score, seconds = state.Difficulty, session.PlayedSeconds(common.GetTimestamp())
		}
		seconds = min(seconds, len(state.Inputs)*5)
		name := "熄灯挑战"
		if key == "hanoi" {
			name = "汉诺塔"
		}
		result := settleSkillSession(c, name, rule, session, skillOutcome{Score: score, Seconds: seconds, State: string(data), Detail: gin.H{"difficulty": state.Difficulty, "moves": len(state.Inputs), "won": won}})
		if result == nil {
			return
		}
		view["status"], view["result"] = model.GameSessionStatusFinished, result
	} else if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
		respondGameError(c, key, err)
		return
	}
	view["version"] = session.Version
	common.ApiSuccess(c, view)
}
