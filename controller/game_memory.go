package controller

import (
	"crypto/rand"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/memory"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const gameMemory = "memory"

func init() {
	registerGame(&gameDefinition{
		Key:  gameMemory,
		Kind: operation_setting.GameKindSkill,
		Info: func(ctx *gameContext) (gin.H, error) {
			info, err := skillInfo(ctx, gameMemory, ctx.Setting.Memory)
			if err != nil {
				return nil, err
			}
			info["levels"] = memory.Levels
			info["symbols"] = memory.SymbolCount
			return info, nil
		},
		Routes: func(group *gin.RouterGroup) {
			group.GET("/state", GetMemoryState)
			group.POST("/start", middleware.GamePlayRateLimit("memory-start"), StartMemory)
			group.POST("/flip", middleware.GameActionRateLimit("memory-flip"), FlipMemory)
		},
	})
}

type memoryStartRequest struct {
	Difficulty int `json:"difficulty"`
}

type memoryFlipRequest struct {
	SessionId int `json:"session_id"`
	Version   int `json:"version"`
	Index     int `json:"index"`
}

// memoryDetail 是记忆翻牌对局的成绩明细，存在对局的 Detail 里；重复提交同一局时据此返回同样的结果。
type memoryDetail struct {
	Difficulty int  `json:"difficulty"`
	Won        bool `json:"won"`
	Moves      int  `json:"moves"`
}

// memoryView 是进行中的一局记忆翻牌给玩家看的样子：牌面大小、步数、已经配上的牌 [位置, 图案]，以及这一步已经翻开的第一张
// face_up({index, symbol}，没有时为 null)。其余牌的图案在一局结束之前绝不返回。
func memoryView(session *model.GameSession, game *memory.Game) gin.H {
	var faceUp gin.H
	if game.First >= 0 {
		faceUp = gin.H{"index": game.First, "symbol": game.Layout[game.First]}
	}
	return gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"difficulty": game.Difficulty,
		"rows":       game.Rows,
		"cols":       game.Cols,
		"pairs":      game.Pairs,
		"move_limit": game.MoveLimit,
		"moves":      game.Moves,
		"matched":    game.MatchedCards(),
		"face_up":    faceUp,
		"status":     game.Status,
		"started_at": session.CreatedAt,
		"elapsed":    common.GetTimestamp() - session.CreatedAt,
	}
}

// StartMemory 开一局记忆翻牌：发好牌、把整副牌存在服务端。洗牌用密码学安全的随机密钥，而不是 32 位的对局种子：种子只有 2^32 种，
// 玩家翻开几张之后就能穷举种子、推出整副牌。
func StartMemory(c *gin.Context) {
	if !requireGameEnabled(c, gameMemory) {
		return
	}
	var req memoryStartRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := memory.LevelOf(req.Difficulty); !ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var game *memory.Game
	session := startSkillSession(c, gameMemory, func(uint32) (string, error) {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return "", err
		}
		var err error
		if game, err = memory.New(req.Difficulty, key); err != nil {
			return "", err
		}
		state, err := common.Marshal(game)
		return string(state), err
	})
	if session == nil {
		return
	}
	common.ApiSuccess(c, memoryView(session, game))
}

// FlipMemory 翻开一张牌，回应这一张的图案；是一步里的第二张时还回应两张有没有配上和第一张是哪张，没配上的两张由浏览器显示
// 一会儿再翻回去。配完所有对或用完步数时这局结束：公开整副牌，按赢下的难度(输了为 0)结算，时长是开局以来的秒数。
// 请求要带玩家看到的版本号，局面已被另一个请求改过时拒绝；只有保存成功之后才回应图案，并发的请求探不到别的牌。
func FlipMemory(c *gin.Context) {
	rule := operation_setting.GetGameSetting().Memory
	if !requireGameEnabled(c, gameMemory) {
		return
	}
	var req memoryFlipRequest
	if err := decodeGameRequest(c, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	session := loadSkillSession(c, gameMemory, rule, req.SessionId)
	if session == nil {
		return
	}
	if req.Version != session.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	var game memory.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.SysError(fmt.Sprintf("failed to decode memory session %d: %v", session.Id, err))
		common.ApiErrorI18n(c, i18n.MsgGamePlayFailed)
		return
	}
	flip, err := game.Flip(req.Index)
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
	if game.Status == memory.StatusPlaying {
		if err := model.UpdateGameSessionState(session, string(state), ""); err != nil {
			respondGameError(c, gameMemory, err)
			return
		}
	} else {
		score := 0
		if game.Status == memory.StatusWon {
			score = game.Difficulty
		}
		result = settleSkillSession(c, "记忆翻牌", rule, session, skillOutcome{
			Score:   score,
			Seconds: session.PlayedSeconds(common.GetTimestamp()),
			Detail:  memoryDetail{Difficulty: game.Difficulty, Won: game.Status == memory.StatusWon, Moves: game.Moves},
			State:   string(state),
		})
		if result == nil {
			return
		}
	}

	view := gin.H{
		"session_id": session.Id,
		"version":    session.Version,
		"index":      flip.Index,
		"symbol":     flip.Symbol,
		"first":      flip.First,
		"moves":      game.Moves,
		"move_limit": game.MoveLimit,
		"status":     game.Status,
	}
	if !flip.First {
		view["matched"] = flip.Matched
		view["previous"] = gin.H{"index": flip.PreviousIndex, "symbol": flip.PreviousSymbol}
	}
	if result != nil {
		view["layout"] = game.Layout
		view["result"] = result
	}
	common.ApiSuccess(c, view)
}

// GetMemoryState 返回玩家进行中的一局记忆翻牌，刷新页面后据此恢复牌面；没有进行中的一局(或已经过期)时 session 为 null。
func GetMemoryState(c *gin.Context) {
	if !requireGameEnabled(c, gameMemory) {
		return
	}
	session, err := model.GetPlayingGameSession(c.GetInt("id"), gameMemory)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var game memory.Game
	if err := common.UnmarshalJsonStr(session.State, &game); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"session": memoryView(session, &game)})
}
