package controller

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

func getSectLineage(c *gin.Context) {
	getSectOutpost(c)
}

// actSectLineage 只给新招的候选指定师承。候选摘要、请求收据和出战占用都在同一存档锁内核对，避免重试多招或旧页面招错人。
func actSectLineage(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		wendao.LineageAction
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Type != "recruit" || req.CandidateSum == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum(struct {
		Kind string               `json:"kind"`
		Body wendao.LineageAction `json:"body"`
	}{Kind: "lineage", Body: req.LineageAction})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now, userId := common.GetTimestamp(), c.GetInt("id")
	allianceLevel, err := model.GetSectAllianceLevel(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var current *wendao.Save
	var events []wendao.Ev
	_, err = model.UpdateGameSaveWithSessions(userId, wendao.SaveKey, []string{sectTournamentGame, sectArenaGame}, now-skillSessionTTL,
		func(data string, sessions []*model.GameSession) (string, error) {
			return wendao.UpdateSave(data, func(save *wendao.Save) error {
				if err := save.Admit(req.RequestId, sum, now); err != nil {
					return err
				}
				busy, err := sectOccupiedDisciples(save, sessions)
				if err != nil {
					return err
				}
				if slices.Contains(busy, req.Teacher) {
					return wendao.ErrUnavailable
				}
				if save.Sect != nil {
					save.Sect.AllianceLevel = allianceLevel
				}
				rollWendaoDaily(save)
				save.TickHero(now)
				events, err = save.LineageApply(req.LineageAction, now)
				current = save
				return err
			})
		})
	replayed := errors.Is(err, wendao.ErrReplayed)
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
	}
	respondSectManagement(c, current, now, events, replayed)
}
