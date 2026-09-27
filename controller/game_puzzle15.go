package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/puzzle15"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gamePuzzle15 = "puzzle15"

const (
	// puzzle15ClockSlackSeconds 容忍开局时间取整到秒以及请求往返带来的误差。
	puzzle15ClockSlackSeconds = 2
	// puzzle15FinishMaxBytes 限制结算请求体：步骤最多 puzzle15.MaxMoves 个字符。
	puzzle15FinishMaxBytes = 64 << 10
)

func init() {
	registerGame(&gameDefinition{
		Key:  gamePuzzle15,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gamePuzzle15, ctx.Setting.Puzzle15)
			if err != nil {
				return nil, err
			}
			info["levels"] = puzzle15.Levels
			info["max_moves"] = puzzle15.MaxMoves
			info["max_moves_per_second"] = puzzle15.MaxMovesPerSecond
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetPuzzle15State)
			group.POST("/start", middleware.GamePlayRateLimit("puzzle15-start"), StartPuzzle15)
			group.POST("/finish", middleware.GamePlayRateLimit("puzzle15-finish"), FinishPuzzle15)
		},
	})
}

type puzzle15StartRequest struct {
	Difficulty int `json:"difficulty"`
}

type puzzle15FinishRequest struct {
	SessionId int    `json:"session_id"`
	Moves     string `json:"moves"`
}

// puzzle15Detail 是数字华容道对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type puzzle15Detail struct {
	Difficulty int `json:"difficulty"`
	Moves      int `json:"moves"`
}

// puzzle15View 是进行中的一局数字华容道给玩家看的样子：难度、边长与开局盘面(按行优先，0 是空格)。这个游戏没有隐藏信息。
func puzzle15View(session *model.GameSession, game *puzzle15.Game) gin.H {
	return gin.H{
		"session_id": session.Id,
		"difficulty": game.Difficulty,
		"size":       game.Size,
		"board":      game.Board,
		"started_at": session.CreatedAt,
		"elapsed":    common.GetTimestamp() - session.CreatedAt,
	}
}

// StartPuzzle15 开一局数字华容道：按这一局的种子打乱出一个可解、还没排好的盘面，直接交给浏览器。
func StartPuzzle15(c *gin.Context) {
	if !requireGameEnabled(c, gamePuzzle15) {
		return
	}
	var req puzzle15StartRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := puzzle15.LevelOf(req.Difficulty); !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var game *puzzle15.Game
	session := startSkillSession(c, gamePuzzle15, func(seed uint32) (string, error) {
		var err error
		if game, err = puzzle15.New(req.Difficulty, uint64(seed)); err != nil {
			return "", err
		}
		state, err := common.Marshal(game)
		return string(state), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, puzzle15View(session, game))
}

// FinishPuzzle15 结算一局数字华容道。浏览器上传空格每一步往哪边走，服务端从开局盘面重放：有一步走出盘面、最后没排好，
// 或者步数快过人手(超过开局以来的秒数乘以每秒 15 步)都不结算；排好了按难度结算，时长是开局以来的秒数。
// 同一局只结算一次，重复提交返回第一次结算的结果。
func FinishPuzzle15(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Puzzle15
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req puzzle15FinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, puzzle15FinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session := loadSkillSession(c, gamePuzzle15, rule, req.SessionId)
	if session == nil {
		return
	}
	elapsed := common.GetTimestamp() - session.CreatedAt
	if int64(len(req.Moves)) > (elapsed+puzzle15ClockSlackSeconds)*puzzle15.MaxMovesPerSecond {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	var game puzzle15.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.SysError(fmt.Sprintf("failed to decode puzzle15 session %d: %v", session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return
	}
	board, err := game.Replay(req.Moves)
	if err != nil || !puzzle15.Solved(board) {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	result := settleSkillSession(c, "数字华容道", rule, session, skillOutcome{
		Score:   game.Difficulty,
		Seconds: int(elapsed),
		Detail:  puzzle15Detail{Difficulty: game.Difficulty, Moves: len(req.Moves)},
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}

// GetPuzzle15State 返回玩家进行中的一局数字华容道的开局盘面，刷新页面后浏览器据此和自己记下的步骤恢复局面；没有进行中的一局
// (或已经过期)时 session 为 null。
func GetPuzzle15State(c *gin.Context) {
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gamePuzzle15)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var game puzzle15.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": puzzle15View(session, &game)})
}
