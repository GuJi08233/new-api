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

func registerMeditationRoutes(group *gin.RouterGroup) {
	group.GET("", getWorldActivities)
	group.POST("/action", middleware.GameActionRateLimit("wendao-meditation"), actMeditation)
}

// actMeditation 的开始、离座与请求收据一起保存；修为按旧活动结清后才切换，不另设一份可重复领取的奖励。
func actMeditation(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Action    string `json:"action"`
		Region    *int   `json:"region,omitempty"`
		Token     string `json:"token,omitempty"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || (req.Action != "start" && req.Action != "leave") ||
		(req.Action == "start" && req.Region == nil) || (req.Action == "leave" && req.Token == "") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	requestId := req.RequestId
	req.RequestId = ""
	// 同一份收据也用于游历；加上活动种类，不能拿游历的 start 编号伪装成打坐请求。
	sum, err := wendao.RequestSum(struct {
		Kind string `json:"kind"`
		Body any    `json:"body"`
	}{Kind: "meditation", Body: req})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now, userId := common.GetTimestamp(), c.GetInt("id")
	var current *wendao.Save
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
				if err := save.StartMeditation(*req.Region, now); err != nil {
					return err
				}
			} else if err := save.LeaveMeditation(req.Token, now); err != nil {
				return err
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
	answer := worldActivityAnswer(current, now, playing != nil && now-playing.CreatedAt <= riftRunTTL)
	if replayed {
		answer["replayed"] = true
	}
	common.ApiSuccess(c, answer)
}
