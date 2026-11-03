package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

func registerTravelRoutes(group *gin.RouterGroup) {
	group.GET("", getTravel)
	group.POST("/action", middleware.GameActionRateLimit("wendao-travel"), actTravel)
}

// travelAnswer 与历练共用主角和储物处概况，游历只预览收益，结束时才真正入账。
func travelAnswer(save *wendao.Save, now int64, riftActive bool) gin.H {
	answer := heroView(save, now, gameCenterDay())
	answer["travel"], answer["rift_active"] = save.TravelView(now), riftActive
	return answer
}

func getTravel(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	playing, err := model.GetPlayingGameSession(c.GetInt("id"), riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	save.TickHero(now)
	common.ApiSuccess(c, travelAnswer(save, now, playing != nil && now-playing.CreatedAt <= riftRunTTL))
}

// actTravel 的奖励、行程 token 与请求收据在同一个事务里改写；开始游历与九幽开局还锁住同一份存档，堵住双页面同时出发。
func actTravel(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Action    string `json:"action"`
		Region    *int   `json:"region,omitempty"`
		Token     string `json:"token,omitempty"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || (req.Action != "start" && req.Action != "finish") ||
		(req.Action == "start" && req.Region == nil) || (req.Action == "finish" && req.Token == "") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	requestId := req.RequestId
	req.RequestId = ""
	sum, err := wendao.RequestSum(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now, userId := common.GetTimestamp(), c.GetInt("id")
	var current *wendao.Save
	var loot *wendao.TravelLoot
	update := func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(requestId, sum, now); err != nil {
				return err
			}
			if save.Hero == nil || save.Sect == nil {
				return wendao.ErrUnavailable
			}
			rollWendaoDaily(save)
			save.Sect.Tick(now)
			if req.Action == "start" {
				if err := save.StartTravel(*req.Region, now); err != nil {
					return err
				}
			} else {
				var err error
				loot, err = save.FinishTravel(req.Token, now)
				if err != nil {
					return err
				}
			}
			current = save
			return nil
		})
	}
	if req.Action == "start" {
		_, err = model.UpdateGameSaveWithoutSession(userId, wendao.SaveKey, riftGame, now-riftRunTTL, update)
	} else {
		_, err = model.UpdateGameSave(userId, wendao.SaveKey, update)
	}
	replayed := errors.Is(err, wendao.ErrReplayed)
	if errors.Is(err, model.ErrGameRoundOpen) {
		common.ApiErrorI18n(c, i18n.MsgGameRoundOpen)
		return
	}
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	if replayed {
		current, err = loadWendaoSave(userId)
		if err != nil {
			respondSectError(c, err)
			return
		}
		current.TickHero(now)
	}
	playing, err := model.GetPlayingGameSession(userId, riftGame)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	answer := travelAnswer(current, now, playing != nil && now-playing.CreatedAt <= riftRunTTL)
	if replayed {
		answer["replayed"] = true
	} else if loot != nil {
		answer["loot"] = loot
	}
	common.ApiSuccess(c, answer)
}
