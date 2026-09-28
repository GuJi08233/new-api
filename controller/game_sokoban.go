package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sokoban"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const gameSokoban = "sokoban"

// sokobanSecondsPerMove 是每一步最多算作多少秒游戏时长：推箱子要边想边走，但浏览器只在最后交一次，开局之后挂着不动不算在玩。
const sokobanSecondsPerMove = 10

func init() {
	registerGame(&gameDefinition{Key: gameSokoban, Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gameSokoban, ctx.Setting.Sokoban)
			if err != nil {
				return nil, err
			}
			info["day"] = ctx.Day
			info["max_moves"] = sokoban.MaxMoves
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetSokobanState)
			group.POST("/start", middleware.GamePlayRateLimit("sokoban-start"), StartSokoban)
			group.POST("/finish", middleware.GamePlayRateLimit("sokoban-finish"), FinishSokoban)
		},
	})
}

func sokobanView(session *model.GameSession, game *sokoban.Game) gin.H {
	return gin.H{"session_id": session.Id, "puzzle": game, "elapsed": max(int64(0), common.GetTimestamp()-session.CreatedAt)}
}

func StartSokoban(c *gin.Context) {
	if !requireGameEnabled(c, gameSokoban) {
		return
	}
	var game *sokoban.Game
	session := startSkillSession(c, gameSokoban, func(uint32) (string, error) {
		var err error
		game, err = sokoban.New(gameCenterDay())
		if err != nil {
			return "", err
		}
		state, err := common.Marshal(game)
		return string(state), err
	})
	if session != nil {
		common.ApiSuccess(c, sokobanView(session, game))
	}
}

func FinishSokoban(c *gin.Context) {
	if !requireGameEnabled(c, gameSokoban) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Moves     string `json:"moves"`
		Abandon   bool   `json:"abandon"`
	}
	if common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10), &req) != nil || len(req.Moves) > sokoban.MaxMoves {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	rule := operation_setting.GetGameSetting().Sokoban
	session := loadSkillSession(c, gameSokoban, rule, req.SessionId)
	if session == nil {
		return
	}
	elapsed := max(int64(0), common.GetTimestamp()-session.CreatedAt)
	if int64(len(req.Moves)) > (elapsed+2)*sokoban.MaxMovesPerSecond {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	var game sokoban.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	// 奖励记在结算的这一天：开局时的那张图已经不是今天的了，解开它不能领今天的奖励，不然一张图的解法能领两天。
	if game.Day != gameCenterDay() {
		common.ApiErrorI18n(c, i18n.MsgGameSessionExpired)
		return
	}
	played := &game
	if len(req.Moves) > 0 {
		var err error
		played, err = game.Replay(req.Moves)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
	}
	if !req.Abandon && !played.Solved() {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	score := 0
	if played.Solved() && !req.Abandon {
		score = 1
	}
	result := settleSkillSession(c, "每日推箱子", rule, session, skillOutcome{Score: score, Seconds: int(min(elapsed, int64(len(req.Moves))*sokobanSecondsPerMove)), Detail: gin.H{"day": game.Day, "moves": len(req.Moves), "pushes": played.Pushes, "won": score > 0}})
	if result != nil {
		common.ApiSuccess(c, result)
	}
}

func GetSokobanState(c *gin.Context) {
	if !requireGameEnabled(c, gameSokoban) {
		return
	}
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gameSokoban)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var game sokoban.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	// 昨天开的一局已经交不了，不再恢复，玩家直接开今天的这张图。
	if game.Day != gameCenterDay() {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	common.ApiSuccess(c, gin.H{"session": sokobanView(session, &game)})
}
