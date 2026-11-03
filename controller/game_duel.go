package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// 斗法契约的接口(规则见 pkg/wendao/duel.go)：入门试炼、宗门任务与悬赏。接下契约时服务端排好敌人、发出种子与开局，浏览器
// 打完交操作记录，服务端按种子与开局重放核对，只认重放出来的输赢。发的是宗门资源与贡献，不是游戏币，所以状态记在问道存档里，
// 不进对局表。接下契约就用掉一次挑战，读取状态拿不到已经发出的种子，所以刷新换不来第二次机会。

func registerDuelRoutes(group *gin.RouterGroup) {
	group.GET("/state", getDuelState)
	group.POST("/start", middleware.GameActionRateLimit("duel-start"), startDuel)
	group.POST("/clear", middleware.GameActionRateLimit("duel-clear"), clearDuel)
	group.POST("/giveup", middleware.GameActionRateLimit("duel-giveup"), giveUpDuel)
	group.POST("/arms", middleware.GameActionRateLimit("duel-arms"), equipArms)
}

// duelWorldInfo 是游戏信息里世界的部分：地域与契约的规则，页面写说明用。
func duelWorldInfo() gin.H {
	return gin.H{
		"regions":        wendao.Regions,
		"bounty_slots":   wendao.BountySlots,
		"task_slots":     wendao.TaskSlots,
		"contract_tries": wendao.ContractTries,
		"trial_tries":    wendao.TrialTries,
	}
}

// duelAnswer 是每个契约接口回应里共有的部分：契约、身份，以及(有储物处时)宗门，页面据此更新。
func duelAnswer(save *wendao.Save, now int64) gin.H {
	answer := gin.H{"book": save.DuelView(), "standing": save.PathView(gameCenterDay()), "travel": save.TravelView(now)}
	if save.Sect != nil {
		answer["sect"] = save.Sect.View(now)
	}
	return answer
}

func getDuelState(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	// 只读：在这份副本上把榜单排到今天，下一次改写存档时再记下来，两次排出来的相同。
	rollWendaoDaily(save)
	common.ApiSuccess(c, gin.H{"book": save.DuelView(), "standing": save.PathView(gameCenterDay()), "travel": save.TravelView(common.GetTimestamp())})
}

// startDuel 接下一张契约：kind 是 trial、task 或 bounty，id 是宗门(入门试炼)或契约编号。回应里带着刚发出的房间(种子与开局)，
// 这是唯一一次拿到它的机会；带 request_id 重发同一次请求时(上一次的回应丢了)再给一遍，不再多用一次挑战。
func startDuel(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Kind      string `json:"kind"`
		Id        string `json:"id"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Kind == "" || req.Id == "" {
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
	seed, err := newGameSeed()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	var duel *wendao.Duel
	_, err = model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(requestId, sum, now); err != nil {
				return err
			}
			if save.Hero == nil {
				return wendao.ErrUnavailable
			}
			if err := ensureStorehouse(save, now); err != nil {
				return err
			}
			rollWendaoDaily(save)
			var err error
			duel, err = save.StartDuel(req.Kind, req.Id, seed, now)
			return err
		})
	})
	replayed := errors.Is(err, wendao.ErrReplayed)
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	if replayed {
		if active := save.Duels.Active; active != nil && active.Kind == req.Kind && active.Id == req.Id {
			duel = active
		}
	}
	answer := duelAnswer(save, now)
	if duel != nil {
		answer["duel"] = duel
	}
	if replayed {
		answer["replayed"] = true
	}
	common.ApiSuccess(c, answer)
}

// clearDuel 提交打完的这一场：服务端按发出的种子与开局重放操作记录，必须正好打到结束，用时不能比帧数对应的时长快太多，
// 然后按重放出来的输赢结算，不看浏览器报的结果。
func clearDuel(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Inputs    string `json:"inputs"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil {
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
	userId := c.GetInt("id")
	now := common.GetTimestamp()
	save, err := loadWendaoSave(userId)
	if err != nil {
		respondSectError(c, err)
		return
	}
	// 没有进行中的一场时，要么是另一个页面已经放弃或结算了，要么是上一次提交其实已经结算(回应丢了)：后一种带着同一个
	// request_id 重发，下面的收据认得出来。
	active := save.Duels.Active
	var fight *doufa.Fight
	if active != nil {
		inputs, err := doufa.DecodeInputs(req.Inputs)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
		fight, err = doufa.Replay(active.Seed, active.Setup, inputs)
		if err != nil || !fight.Over || (now-active.Issued+riftGraceSeconds)*5*doufa.FPS < int64(fight.Tick)*4 {
			common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
			return
		}
	}
	var evs []wendao.Ev
	_, err = model.UpdateGameSave(userId, wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(sv *wendao.Save) error {
			if err := sv.Admit(requestId, sum, now); err != nil {
				return err
			}
			rollWendaoDaily(sv)
			// 重放核对的是读存档时发出的那一场；这一刻存档里的还得是它(另一个页面可能已经放弃或结算了)。
			current := sv.Duels.Active
			if sv.Hero == nil || sv.Sect == nil || active == nil || current == nil || current.Seed != active.Seed || current.Issued != active.Issued {
				return wendao.ErrUnavailable
			}
			var err error
			evs, err = sv.FinishDuel(fight, now)
			return err
		})
	})
	replayed := errors.Is(err, wendao.ErrReplayed)
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	save, err = loadWendaoSave(userId)
	if err != nil {
		respondSectError(c, err)
		return
	}
	answer := duelAnswer(save, now)
	if replayed {
		answer["replayed"] = true
	} else {
		answer["won"], answer["kills"], answer["ticks"], answer["events"] = fight.Won, fight.Kills, fight.Tick, evs
	}
	common.ApiSuccess(c, answer)
}

// giveUpDuel 放弃进行中的一场：用掉的那次挑战不退。页面关掉重开以后只能走这一步，因为种子只在接下契约的回应里给一次。
func giveUpDuel(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	sum, err := wendao.RequestSum(struct{}{})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	_, err = model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if err := save.Admit(req.RequestId, sum, now); err != nil {
				return err
			}
			rollWendaoDaily(save)
			// 隔天榜单换了，进行中的一场已经作废，没有什么可放弃的。
			if save.Duels.Active == nil {
				return nil
			}
			return save.GiveUpDuel()
		})
	})
	replayed := errors.Is(err, wendao.ErrReplayed)
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	answer := duelAnswer(save, now)
	if replayed {
		answer["replayed"] = true
	}
	common.ApiSuccess(c, answer)
}

// equipArms 配置出战的法宝与法术(规则见 pkg/wendao/arms.go)：weapons 是一到两件法宝，skill 是法术，都得是主角有的。配置用在
// 悬赏、宗门任务与入门试炼里，带不进九幽秘境。
func equipArms(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Weapons []string `json:"weapons"`
		Skill   string   `json:"skill"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	now := common.GetTimestamp()
	_, err := model.UpdateGameSave(c.GetInt("id"), wendao.SaveKey, func(data string) (string, error) {
		return wendao.UpdateSave(data, func(save *wendao.Save) error {
			if save.Hero == nil {
				return wendao.ErrUnavailable
			}
			return save.Hero.Equip(req.Weapons, req.Skill)
		})
	})
	if err != nil {
		respondSectError(c, err)
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err != nil {
		respondSectError(c, err)
		return
	}
	rollWendaoDaily(save)
	common.ApiSuccess(c, duelAnswer(save, now))
}
