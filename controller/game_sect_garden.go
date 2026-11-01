package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 宗门灵田的串门：自己灵田的种收走宗门的经营操作(actSect)，这里只管去道友的灵田浇灌、摘取。道友的宗门与自己的宗门在同一个
// 事务里改写(model.UpdateTwoGameSaves)，谢礼和摘到的东西只是宗门资源，不发游戏币。

const (
	// gardenRecentNeighbors 是道友列表里列出几位最近上线的掌门，仙盟的同门另外全部列出。
	gardenRecentNeighbors = 12
)

func registerSectGardenRoutes(group *gin.RouterGroup) {
	group.GET("/neighbors", getGardenNeighbors)
	group.GET("/visit", getGardenVisit)
	group.POST("/visit", middleware.GameActionRateLimit("sect-garden-visit"), visitGarden)
}

// getGardenNeighbors 列出能去串门的道友：仙盟的同门在前，再是最近上线的掌门；每位附上现在能浇灌、能摘的地有几块，以及自己
// 今天还剩几次。
func getGardenNeighbors(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	userId := c.GetInt("id")
	now := common.GetTimestamp()
	var ids []int
	alliance, _, err := model.GetSectAllianceOf(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if alliance != nil {
		members, err := model.ListSectAllianceMembers(alliance.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		for _, m := range members {
			if m.UserId != userId {
				ids = append(ids, m.UserId)
			}
		}
	}
	saves, err := model.ListGameSaves(wendao.SaveKey, ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recent, err := model.ListRecentGameSaves(wendao.SaveKey, userId, gardenRecentNeighbors)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	allies := make(map[int]bool, len(ids))
	for _, id := range ids {
		allies[id] = true
	}
	for _, save := range recent {
		if !allies[save.UserId] {
			saves = append(saves, save)
		}
	}
	type neighbor struct {
		UserId int    `json:"user_id"`
		Name   string `json:"name"`
		Ally   bool   `json:"ally"`
		Hall   int    `json:"hall"`
		Plots  int    `json:"plots"`
		Water  int    `json:"water"`
		Pick   int    `json:"pick"`
	}
	list := make([]neighbor, 0, len(saves))
	userIds := make([]int, 0, len(saves))
	for _, save := range saves {
		s, err := wendao.ParseSect(save.Data)
		if err != nil || s == nil {
			continue
		}
		water, pick := s.GardenNeeds(userId, now)
		list = append(list, neighbor{UserId: save.UserId, Ally: allies[save.UserId], Hall: s.Buildings[wendao.BuildingHall],
			Plots: len(s.Garden.Plots), Water: water, Pick: pick})
		userIds = append(userIds, save.UserId)
	}
	names, err := model.GetGameDisplayNames(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range list {
		list[i].Name = names[list[i].UserId]
	}
	view := gin.H{"neighbors": list, "water_left": wendao.GardenWaterDaily, "pick_left": wendao.GardenPickDaily}
	if data, err := model.GetGameSave(userId, wendao.SaveKey); err == nil {
		if s, err := wendao.ParseSect(data); err == nil && s != nil {
			view["water_left"], view["pick_left"] = s.GardenDayLeft(gameCenterDay())
		}
	}
	common.ApiSuccess(c, view)
}

// gardenOwner 读出查询参数里的道友编号，不能是自己。
func gardenOwner(c *gin.Context, raw string) (int, bool) {
	owner, err := strconv.Atoi(raw)
	if err != nil || owner <= 0 || owner == c.GetInt("id") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return 0, false
	}
	return owner, true
}

// getGardenVisit 看一位道友的灵田：每块地种了什么、什么时候熟，自己能不能浇灌、摘取。
func getGardenVisit(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	owner, ok := gardenOwner(c, c.Query("user_id"))
	if !ok {
		return
	}
	data, err := model.GetGameSave(owner, wendao.SaveKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	s, err := wendao.ParseSect(data)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if s == nil {
		common.ApiErrorI18n(c, i18n.MsgGameSectUnavailable)
		return
	}
	names, err := model.GetGameDisplayNames([]int{owner})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	common.ApiSuccess(c, gin.H{"user_id": owner, "name": names[owner], "now": now, "plots": s.VisitView(c.GetInt("id"), now)})
}

// visitGarden 在道友的灵田上浇灌或摘取一块地：道友的灵田与自己宗门的谢礼(或摘到的东西)在同一个事务里记下，任何一边不成立
// (道友的地不能浇、自己今天的次数用完)都整笔不改。
func visitGarden(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		UserId int    `json:"user_id"`
		Index  int    `json:"index"`
		Kind   string `json:"kind"`
	}
	if decodeGameRequest(c, &req) != nil || (req.Kind != "water" && req.Kind != "pick") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	owner, ok := gardenOwner(c, strconv.Itoa(req.UserId))
	if !ok {
		return
	}
	visitor := c.GetInt("id")
	names, err := model.GetGameDisplayNames([]int{visitor})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	day := gameCenterDay()
	var evs []wendao.Ev
	var plots []wendao.PlotView
	var mine *wendao.Sect
	err = model.UpdateTwoGameSaves(owner, visitor, wendao.SaveKey, func(ownerData string, visitorData string) (string, string, error) {
		ownerSave, err := wendao.ParseSave(ownerData)
		if err != nil {
			return "", "", err
		}
		visitorSave, err := wendao.ParseSave(visitorData)
		if err != nil {
			return "", "", err
		}
		if ownerSave.Sect == nil || visitorSave.Sect == nil {
			return "", "", wendao.ErrUnavailable
		}
		var got wendao.Cost
		if req.Kind == "water" {
			err = ownerSave.Sect.WaterFor(visitor, names[visitor], req.Index, now)
		} else {
			got, err = ownerSave.Sect.PickFrom(visitor, names[visitor], req.Index, now)
		}
		if err != nil {
			return "", "", err
		}
		if evs, err = visitorSave.Sect.GardenThanks(req.Kind, got, day); err != nil {
			return "", "", err
		}
		if req.Kind == "water" {
			rollWendaoDaily(visitorSave)
			visitorSave.Sect.Progress(wendao.TaskWater, 1)
		}
		plots, mine = ownerSave.Sect.VisitView(visitor, now), visitorSave.Sect
		ownerEncoded, err := ownerSave.Encode()
		if err != nil {
			return "", "", err
		}
		visitorEncoded, err := visitorSave.Encode()
		return ownerEncoded, visitorEncoded, err
	})
	if err != nil {
		respondSectError(c, err)
		return
	}
	if evs == nil {
		evs = []wendao.Ev{}
	}
	evs = append([]wendao.Ev{{K: "garden_" + req.Kind, N: req.Index}}, evs...)
	water, pick := mine.GardenDayLeft(day)
	common.ApiSuccess(c, gin.H{"plots": plots, "events": evs, "water_left": water, "pick_left": pick, "now": now})
}
