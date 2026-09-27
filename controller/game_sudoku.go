package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sudoku"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameSudoku = "sudoku"

// sudokuFinishMaxBytes 限制交卷请求的请求体：答卷只有 81 个数字。
const sudokuFinishMaxBytes = 4 << 10

func init() {
	registerGame(&gameDefinition{
		Key:  gameSudoku,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gameSudoku, ctx.Setting.Sudoku)
			if err != nil {
				return nil, err
			}
			info["levels"] = sudoku.Levels
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetSudokuState)
			group.POST("/start", middleware.GamePlayRateLimit("sudoku-start"), StartSudoku)
			group.POST("/finish", middleware.GamePlayRateLimit("sudoku-finish"), FinishSudoku)
			group.POST("/abandon", middleware.GamePlayRateLimit("sudoku-abandon"), AbandonSudoku)
		},
	})
}

type sudokuStartRequest struct {
	Difficulty int `json:"difficulty"`
}

type sudokuFinishRequest struct {
	SessionId int    `json:"session_id"`
	Grid      string `json:"grid"`
}

type sudokuAbandonRequest struct {
	SessionId int `json:"session_id"`
}

// sudokuDetail 是数独对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type sudokuDetail struct {
	Difficulty int  `json:"difficulty"`
	Givens     int  `json:"givens"`
	Solved     bool `json:"solved"`
}

// sudokuView 是进行中的一局数独给玩家看的样子：难度与题目(81 个字符，'0' 是空格)。谜底只存在服务端。
func sudokuView(session *model.GameSession, puzzle *sudoku.Puzzle) gin.H {
	return gin.H{
		"session_id": session.Id,
		"difficulty": puzzle.Difficulty,
		"puzzle":     puzzle.Puzzle,
		"started_at": session.CreatedAt,
		"elapsed":    common.GetTimestamp() - session.CreatedAt,
	}
}

// StartSudoku 开一局数独：按这一局的种子出一道唯一解的题，题目交给浏览器，谜底只存在服务端。
func StartSudoku(c *gin.Context) {
	if !requireGameEnabled(c, gameSudoku) {
		return
	}
	var req sudokuStartRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := sudoku.LevelOf(req.Difficulty); !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var puzzle *sudoku.Puzzle
	session := startSkillSession(c, gameSudoku, func(seed uint32) (string, error) {
		var err error
		if puzzle, err = sudoku.Generate(req.Difficulty, uint64(seed)); err != nil {
			return "", err
		}
		state, err := common.Marshal(puzzle)
		return string(state), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, sudokuView(session, puzzle))
}

// loadSudokuSession 读出玩家要交卷或放弃的一局数独与它的题目；已经结算过时回应第一次结算的结果。返回 nil 时已写好响应。
func loadSudokuSession(c *gin.Context, rule operation_setting.SkillRule, sessionId int) (*model.GameSession, *sudoku.Puzzle) {
	session := loadSkillSession(c, gameSudoku, rule, sessionId)
	if session == nil {
		return nil, nil
	}
	var puzzle sudoku.Puzzle
	if err := common.UnmarshalJsonStr(session.State, &puzzle); err != nil {
		common.SysError(fmt.Sprintf("failed to decode sudoku session %d: %v", session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return nil, nil
	}
	return session, &puzzle
}

// FinishSudoku 交卷：给出的数字没改、每行每列每宫 1-9 各一次，就按解出的难度结算，时长是开局以来的秒数。答卷不对时不结算，
// 玩家可以接着改了再交。同一局只结算一次，重复提交返回第一次结算的结果。
func FinishSudoku(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Sudoku
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req sudokuFinishRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, sudokuFinishMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	session, puzzle := loadSudokuSession(c, rule, req.SessionId)
	if session == nil {
		return
	}
	if !sudoku.Check(puzzle.Puzzle, req.Grid) {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	result := settleSkillSession(c, "数独", rule, session, skillOutcome{
		Score:   puzzle.Difficulty,
		Seconds: int(common.GetTimestamp() - session.CreatedAt),
		Detail:  sudokuDetail{Difficulty: puzzle.Difficulty, Givens: puzzle.Givens(), Solved: true},
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}

// AbandonSudoku 放弃这一局，按成绩 0 结算。
func AbandonSudoku(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Sudoku
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req sudokuAbandonRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	session, puzzle := loadSudokuSession(c, rule, req.SessionId)
	if session == nil {
		return
	}
	result := settleSkillSession(c, "数独", rule, session, skillOutcome{
		Score:   0,
		Seconds: int(common.GetTimestamp() - session.CreatedAt),
		Detail:  sudokuDetail{Difficulty: puzzle.Difficulty, Givens: puzzle.Givens(), Solved: false},
	})
	if result == nil {
		return
	}
	common.ApiSuccess(c, result)
}

// GetSudokuState 返回玩家进行中的一局数独，刷新页面后据此恢复题目；没有进行中的一局(或已经过期)时 session 为 null。
// 玩家填到一半的数字只在浏览器里。
func GetSudokuState(c *gin.Context) {
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gameSudoku)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var puzzle sudoku.Puzzle
	if err := common.UnmarshalJsonStr(session.State, &puzzle); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": sudokuView(session, &puzzle)})
}
