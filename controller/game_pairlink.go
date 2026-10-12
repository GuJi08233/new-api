package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/pairlink"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const gamePairlink = "pairlink"

func init() {
	registerGame(&gameDefinition{Key: gamePairlink, Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gamePairlink, ctx.Setting.Pairlink)
			if err != nil {
				return nil, err
			}
			info["levels"] = pairlink.Levels
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetPairlinkState)
			group.POST("/start", middleware.GamePlayRateLimit("pairlink-start"), StartPairlink)
			group.POST("/action", middleware.GameActionRateLimit("pairlink-action"), ActPairlink)
		},
	})
}

func pairlinkView(session *model.GameSession, game *pairlink.Game) gin.H {
	return gin.H{"session_id": session.Id, "version": session.Version, "difficulty": game.Difficulty, "rows": game.Rows, "cols": game.Cols,
		"board": game.Board, "remaining": game.Remaining, "hints": game.Hints, "shuffles": game.Shuffles, "status": game.Status,
		"elapsed": max(int64(0), common.GetTimestamp()-session.CreatedAt), "seconds": game.Seconds}
}

func StartPairlink(c *gin.Context) {
	if !requireGameEnabled(c, gamePairlink) {
		return
	}
	var req struct {
		Difficulty int `json:"difficulty"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &req) != nil || req.Difficulty < 1 || req.Difficulty > len(pairlink.Levels) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var game *pairlink.Game
	session := startSkillSession(c, gamePairlink, func(seed uint32) (string, error) {
		var err error
		game, err = pairlink.New(req.Difficulty, uint64(seed))
		if err != nil {
			return "", err
		}
		state, err := common.Marshal(game)
		return string(state), err
	})
	if session != nil {
		common.ApiSuccess(c, pairlinkView(session, game))
	}
}

func ActPairlink(c *gin.Context) {
	if !requireGameEnabled(c, gamePairlink) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		First     int    `json:"first"`
		Second    int    `json:"second"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule := operation_setting.GetGameSetting().Pairlink
	session := loadSkillSession(c, gamePairlink, rule, req.SessionId)
	if session == nil {
		return
	}
	if req.Version != session.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	var game pairlink.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	elapsed := max(int64(0), common.GetTimestamp()-session.CreatedAt)
	var path []pairlink.Point
	var hint []int
	shuffled := false
	// 开局与现在都按整秒计，算出来的时间最多差一秒；到点之后的这一秒仍然算数，页面倒计时还剩时间时发出的最后一步不会被判输。
	if elapsed > int64(game.Seconds) || req.Action == "finish" {
		game.Status = "lost"
	} else {
		switch req.Action {
		case "match":
			// 人类操作速率与结算时长分开检查，不能瞬间批量翻完再挂机领奖。
			if int64(game.Rows*game.Cols/2-game.Remaining+1) > (elapsed+2)*4 {
				common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
				return
			}
			var err error
			path, shuffled, err = game.Match(req.First, req.Second)
			if err != nil {
				common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
				return
			}
		case "hint":
			if game.Hints == 0 {
				common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
				return
			}
			game.Hints--
			hint = game.Hint()
		case "shuffle":
			if game.Shuffles == 0 {
				common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
				return
			}
			game.Shuffles--
			game.Shuffle()
			shuffled = true
		default:
			common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
			return
		}
	}
	state, err := common.Marshal(&game)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var result gin.H
	if game.Status == "playing" {
		if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
			respondGameError(c, gamePairlink, err)
			return
		}
	} else {
		pairs := game.Rows*game.Cols/2 - game.Remaining
		score := 0
		// 放弃或超时只保留已保存操作的时长，不把结束前的空等追加进去。
		// 仅用提示或重排还不能证明玩过：至少成功消除过一对才累计本局时长。
		seconds := 0
		if pairs > 0 {
			seconds = session.PlaySeconds
		}
		if game.Status == "won" {
			score = game.Difficulty
			seconds = session.PlayedSeconds(common.GetTimestamp())
		}
		result = settleSkillSession(c, "连连看", rule, session, skillOutcome{Score: score, Seconds: min(seconds, game.Seconds), State: string(state), Detail: gin.H{"difficulty": game.Difficulty, "won": game.Status == "won", "pairs": pairs}})
		if result == nil {
			return
		}
	}
	view := pairlinkView(session, &game)
	view["path"], view["hint"], view["shuffled"], view["result"] = path, hint, shuffled, result
	common.ApiSuccess(c, view)
}

func GetPairlinkState(c *gin.Context) {
	if !requireGameEnabled(c, gamePairlink) {
		return
	}
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gamePairlink)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var game pairlink.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": pairlinkView(session, &game)})
}
