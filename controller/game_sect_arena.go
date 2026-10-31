package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/sect"
	"github.com/gin-gonic/gin"
)

// 问道宗门的论剑：掌门之间的异步对战。守擂阵容与积分存在 sect_arenas 表里，一场挑战是一局存在 sect_arena 标识下的对局，
// 规则与大比的斗法相同(见 pkg/sect/arena.go)。论剑不发游戏币，只发宗门资源；积分只用来排名与匹配对手。

const sectArenaGame = "sect_arena"

const (
	// sectArenaOpponents 是论剑页列出几个积分相近的对手。
	sectArenaOpponents = 6
	// sectArenaTop 是论剑榜显示前几名。
	sectArenaTop = 20
	// sectArenaBattles 是最近战报显示几条。
	sectArenaBattles = 10
)

// sectArenaLineup 读出守擂阵容(弟子编号)。
func sectArenaLineup(row *model.SectArena) []int {
	var ids []int
	if row == nil || row.Lineup == "" {
		return ids
	}
	if err := common.UnmarshalJsonStr(row.Lineup, &ids); err != nil {
		return nil
	}
	return ids
}

// loadSectArenaDefenders 按守擂宗门现在的实力排出它的守擂弟子。没有宗门、没设阵容或阵容里的弟子都走了时返回空。
func loadSectArenaDefenders(row *model.SectArena) ([]*sect.Fighter, error) {
	ids := sectArenaLineup(row)
	if len(ids) == 0 {
		return nil, nil
	}
	data, err := model.GetGameSave(row.UserId, sectGame)
	if err != nil {
		return nil, err
	}
	s, err := sect.ParseSect(data)
	if err != nil || s == nil {
		return nil, err
	}
	return s.ArenaDefenders(ids), nil
}

// getSectArena 是论剑页：自己的积分与守擂阵容、积分相近的对手(连同他们的守擂弟子)、论剑榜与最近战报。
func getSectArena(c *gin.Context) {
	if !requireGameEnabled(c, sectGame) {
		return
	}
	userId := c.GetInt("id")
	me, err := model.GetSectArena(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rating := model.SectArenaStartRating
	var mine gin.H
	if me != nil {
		rating = me.Rating
		defenders, err := loadSectArenaDefenders(me)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		// 自己的守擂战力按现在的实力刷新，别人在榜上看到的是最近一次算出的值。
		if power := sect.ArenaPower(defenders); power != me.Power && len(defenders) > 0 {
			if err := model.UpdateSectArenaPower(userId, power); err != nil {
				common.ApiError(c, err)
				return
			}
			me.Power = power
		}
		mine = gin.H{"row": me, "lineup": sectArenaLineup(me)}
	}
	opponents, err := model.ListSectArenaOpponents(userId, rating, sectArenaOpponents)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	top, err := model.ListSectArenaTop(sectArenaTop)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	battles, err := model.ListSectArenaBattles(userId, sectArenaBattles)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := []int{userId}
	for _, row := range append(append([]model.SectArena{}, opponents...), top...) {
		ids = append(ids, row.UserId)
	}
	for _, battle := range battles {
		ids = append(ids, battle.Attacker, battle.Defender)
	}
	names, err := model.GetGameDisplayNames(ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	opponentViews := make([]gin.H, 0, len(opponents))
	for i := range opponents {
		defenders, err := loadSectArenaDefenders(&opponents[i])
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if len(defenders) == 0 {
			continue
		}
		opponentViews = append(opponentViews, gin.H{"row": opponents[i], "defenders": defenders})
	}
	common.ApiSuccess(c, gin.H{
		"self":       userId,
		"me":         mine,
		"opponents":  opponentViews,
		"top":        top,
		"battles":    battles,
		"names":      names,
		"daily_runs": sect.ArenaDailyRuns,
	})
}

// setSectArenaDefense 设置守擂阵容：一到三名宗门里的弟子(在外历练或受伤也照常守擂)。第一次设置时上论剑榜。
func setSectArenaDefense(c *gin.Context) {
	if !requireGameEnabled(c, sectGame) {
		return
	}
	var req struct {
		Team []int `json:"team"`
	}
	if decodeGameRequest(c, &req) != nil || len(req.Team) == 0 || len(req.Team) > sect.TeamSize {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	data, err := model.GetGameSave(userId, sectGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	s, err := sect.ParseSect(data)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if s == nil {
		common.ApiErrorI18n(c, i18n.MsgGameSectUnavailable)
		return
	}
	lineup, err := s.ArenaLineup(req.Team)
	if err != nil {
		respondSectError(c, err)
		return
	}
	encoded, err := common.Marshal(lineup)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	row, err := model.SetSectArenaDefense(userId, string(encoded), sect.ArenaPower(s.ArenaDefenders(lineup)))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"row": row, "lineup": lineup})
}

func getSectArenaBattle(c *gin.Context) { getSectDuel(c, sectArenaGame) }

// startSectArena 带选好的弟子挑战另一个宗门的守擂阵容。守擂弟子按对方宗门现在的实力上场。
func startSectArena(c *gin.Context) {
	if !requireGameEnabled(c, sectGame) {
		return
	}
	var req struct {
		Opponent int   `json:"opponent"`
		Team     []int `json:"team"`
	}
	if decodeGameRequest(c, &req) != nil || len(req.Team) == 0 || len(req.Team) > sect.TeamSize ||
		req.Opponent <= 0 || req.Opponent == c.GetInt("id") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	row, err := model.GetSectArena(req.Opponent)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defenders, err := loadSectArenaDefenders(row)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(defenders) == 0 {
		common.ApiErrorI18n(c, i18n.MsgGameSectUnavailable)
		return
	}
	startSectDuel(c, sectArenaGame, req.Opponent, func(seed uint64, s *sect.Sect, now int64) (*sect.Tournament, error) {
		return sect.NewArenaBattle(seed, s, req.Team, defenders, now)
	})
}

func actSectArena(c *gin.Context) { actSectDuel(c, sectArenaGame) }
