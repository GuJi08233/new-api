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

// sectOccupiedDisciples 只占用主动出战的弟子；论剑守擂沿用虚影名单，不占用本宗或下院的实际弟子。
func sectOccupiedDisciples(save *wendao.Save, sessions []*model.GameSession) ([]int, error) {
	busy := []int{}
	if save.Hero != nil && save.Hero.Battle != nil {
		busy = append(busy, save.Hero.Battle.Team...)
	}
	for _, session := range sessions {
		state, err := parseSectTournament(session)
		if err != nil {
			return nil, err
		}
		for _, fighter := range state.Tournament.Fighters {
			if fighter != nil && fighter.Id > 0 && !slices.Contains(busy, fighter.Id) {
				busy = append(busy, fighter.Id)
			}
		}
	}
	return busy, nil
}

// respondSectManagement 让同步与下院操作使用同一视图，出战占用只是提示，写操作仍在存档锁内重新判断。
func respondSectManagement(c *gin.Context, save *wendao.Save, now int64, events []wendao.Ev, replayed bool) {
	sessions, err := model.GetPlayingGameSessions(c.GetInt("id"), []string{sectTournamentGame, sectArenaGame}, now-skillSessionTTL)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view := save.OutpostView(now)
	lineage := save.LineageView(now)
	if view != nil || lineage != nil {
		busy, busyErr := sectOccupiedDisciples(save, sessions)
		if busyErr != nil {
			common.ApiError(c, busyErr)
			return
		}
		if view != nil {
			view.Busy = busy
		}
		if lineage != nil {
			lineage.Busy = busy
		}
	}
	if events == nil {
		events = []wendao.Ev{}
	}
	answer := gin.H{"sect": nil, "standing": save.PathView(gameCenterDay()), "outposts": view, "lineage": lineage, "events": events}
	if save.Sect != nil {
		answer["sect"] = save.Sect.View(now)
	}
	if replayed {
		answer["replayed"] = true
	}
	common.ApiSuccess(c, answer)
}

func getSectOutpost(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	now := common.GetTimestamp()
	rollWendaoDaily(save)
	if save.Sect != nil {
		save.Sect.Tick(now)
	}
	save.TickHero(now)
	respondSectManagement(c, save, now, nil, false)
}

// actSectOutpost 的行程 token、收据和资源改动同事务保存；派驻与大比/主动论剑开局共用存档锁，不能穿插双用同一弟子。
func actSectOutpost(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		wendao.OutpostAction
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || (req.Type != "build" && req.Type != "station" && req.Type != "recall") {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum(struct {
		Kind string               `json:"kind"`
		Body wendao.OutpostAction `json:"body"`
	}{Kind: "outpost", Body: req.OutpostAction})
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
				if req.Type == "station" {
					busy, err := sectOccupiedDisciples(save, sessions)
					if err != nil {
						return err
					}
					if slices.Contains(busy, req.Disciple) {
						return wendao.ErrUnavailable
					}
				}
				if save.Sect != nil {
					save.Sect.AllianceLevel = allianceLevel
				}
				rollWendaoDaily(save)
				save.TickHero(now)
				var err error
				events, err = save.OutpostApply(req.OutpostAction, now)
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
