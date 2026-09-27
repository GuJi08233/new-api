package controller

import (
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/minesweeper"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameMinesweeper = "minesweeper"

// minesweeperRevealMaxBytes 限制翻开请求的请求体：一次最多翻开 minesweeper.MaxRevealCells 格，几百字节就够了。
const minesweeperRevealMaxBytes = 4 << 10

func init() {
	registerGame(&gameDefinition{
		Key:  gameMinesweeper,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gameMinesweeper, ctx.Setting.Minesweeper)
			if err != nil {
				return nil, err
			}
			info["levels"] = minesweeper.Levels
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetMinesweeperState)
			group.POST("/start", middleware.GamePlayRateLimit("minesweeper-start"), StartMinesweeper)
			group.POST("/reveal", middleware.GameActionRateLimit("minesweeper-reveal"), RevealMinesweeper)
		},
	})
}

type minesweeperStartRequest struct {
	Difficulty int `json:"difficulty"`
}

type minesweeperRevealRequest struct {
	SessionId int     `json:"session_id"`
	Version   int     `json:"version"`
	Cells     [][]int `json:"cells"`
}

// minesweeperDetail 是扫雷对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type minesweeperDetail struct {
	Difficulty int  `json:"difficulty"`
	Won        bool `json:"won"`
	Opened     int  `json:"opened"`
}

// minesweeperView 是进行中的一局扫雷给玩家看的样子：雷区大小、雷数与已经翻开的格子。雷的位置在一局结束之前绝不返回。
func minesweeperView(session *model.GameSession, game *minesweeper.Game) gin.H {
	return gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"difficulty": game.Difficulty,
		"width":      game.Width,
		"height":     game.Height,
		"mines":      game.Mines,
		"status":     game.Status,
		"revealed":   game.Revealed(),
		"started_at": session.CreatedAt,
		"elapsed":    common.GetTimestamp() - session.CreatedAt,
	}
}

// StartMinesweeper 开一局扫雷。雷要等第一次翻开时才布置(第一下一定不踩雷)，开局只记下难度和布雷用的随机密钥。
// 密钥取自密码学安全的随机数，而不是 32 位的对局种子：种子只有 2^32 种，玩家翻开一片之后就能穷举种子、对照数字推出整片雷区。
func StartMinesweeper(c *gin.Context) {
	if !requireGameEnabled(c, gameMinesweeper) {
		return
	}
	var req minesweeperStartRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := minesweeper.LevelOf(req.Difficulty); !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var game *minesweeper.Game
	session := startSkillSession(c, gameMinesweeper, func(uint32) (string, error) {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return "", err
		}
		var err error
		if game, err = minesweeper.New(req.Difficulty, key); err != nil {
			return "", err
		}
		state, err := common.Marshal(game)
		return string(state), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, minesweeperView(session, game))
}

// RevealMinesweeper 按顺序翻开玩家点的格子(双击和弦时是一个数字周围没插旗的几格)，只回应这次新翻开的格子。踩雷或翻开所有
// 不是雷的格子时这局结束：公开全部雷的位置，按赢下的难度(踩雷为 0)结算，时长是开局以来的秒数。请求要带玩家看到的版本号，
// 局面已被另一个请求改过时拒绝，浏览器重新拉取局面；只有保存成功之后才回应翻开的格子，并发的请求探不到别的格子。
func RevealMinesweeper(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Minesweeper
	if !rule.Enabled {
		common.ApiErrorI18n(c, i18n.MsgGamePlayDisabled)
		return
	}
	var req minesweeperRevealRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, minesweeperRevealMaxBytes), &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	cells := make([][2]int, 0, len(req.Cells))
	for _, cell := range req.Cells {
		if len(cell) != 2 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		cells = append(cells, [2]int{cell[0], cell[1]})
	}
	session := loadSkillSession(c, gameMinesweeper, rule, req.SessionId)
	if session == nil {
		return
	}
	if req.Version != session.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	var game minesweeper.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.SysError(fmt.Sprintf("failed to decode minesweeper session %d: %v", session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return
	}
	revealed, err := game.Reveal(cells)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	state, err := common.Marshal(&game)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var result gin.H
	if game.Status == minesweeper.StatusPlaying {
		// 点的都是已经翻开的格子时局面没变，不用保存。
		if len(revealed) > 0 {
			if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
				respondGameError(c, gameMinesweeper, err)
				return
			}
		}
	} else {
		score := 0
		if game.Status == minesweeper.StatusWon {
			score = game.Difficulty
		}
		result = settleSkillSession(c, "扫雷", rule, session, skillOutcome{
			Score:   score,
			Seconds: int(common.GetTimestamp() - session.CreatedAt),
			Detail:  minesweeperDetail{Difficulty: game.Difficulty, Won: game.Status == minesweeper.StatusWon, Opened: len(game.Revealed())},
			State:   string(state),
		})
		if result == nil {
			return
		}
	}

	view := gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"status":     game.Status,
		"revealed":   revealed,
	}
	if result != nil {
		view["mine_cells"] = game.MineCells()
		if exploded, ok := game.Exploded(); ok {
			view["exploded"] = exploded
		}
		view["result"] = result
	}
	common.ApiSuccess(c, view)
}

// GetMinesweeperState 返回玩家进行中的一局扫雷，刷新页面后据此恢复雷区；没有进行中的一局(或已经过期)时 session 为 null。
func GetMinesweeperState(c *gin.Context) {
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gameMinesweeper)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var game minesweeper.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": minesweeperView(session, &game)})
}
