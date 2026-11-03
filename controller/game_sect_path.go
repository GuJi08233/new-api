package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/gin-gonic/gin"
)

// actSectPath 改主角的身份(规则见 pkg/wendao/path.go)：join 拜入 sect 这个正道宗门，leave 退出，found 开宗立派，
// stipend 领今天的俸禄，donate 上交 stone/herb/ore 换贡献，exchange 用贡献换宝库里的 goods。带 request_id 时重发同一次
// 请求不做第二遍(见 wendao.Save.Admit)。回应里是储物处(或宗门)、身份(standing)与这次发生的事。
func actSectPath(c *gin.Context) {
	if !requireGameEnabled(c, wendaoGame) {
		return
	}
	var req struct {
		Action    string `json:"action"`
		Sect      string `json:"sect"`
		Goods     string `json:"goods"`
		Stone     int    `json:"stone"`
		Herb      int    `json:"herb"`
		Ore       int    `json:"ore"`
		RequestId string `json:"request_id,omitempty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Action == "" {
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
	now := common.GetTimestamp()
	day := gameCenterDay()
	var evs []wendao.Ev
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
			save.TickHero(now)
			rollWendaoDaily(save)
			save.Sect.Tick(now)
			var err error
			switch req.Action {
			case "join":
				if err = save.JoinSect(req.Sect, now); err == nil {
					evs = []wendao.Ev{{K: "path_join", S: req.Sect}}
				}
			case "leave":
				left := save.PathView(day)
				if err = save.LeaveSect(now); err == nil {
					evs = []wendao.Ev{{K: "path_leave", S: left.Path.Sect}}
				}
			case "found":
				evs, err = save.FoundSect(now)
			case "stipend":
				evs, err = save.ClaimStipend(day)
			case "donate":
				evs, err = save.Donate(wendao.Cost{Stone: req.Stone, Herb: req.Herb, Ore: req.Ore})
			case "exchange":
				evs, err = save.Exchange(req.Goods)
			default:
				err = wendao.ErrAction
			}
			return err
		})
	})
	replayed := errors.Is(err, wendao.ErrReplayed)
	if err != nil && !replayed {
		respondSectError(c, err)
		return
	}
	save, err := loadWendaoSave(c.GetInt("id"))
	if err == nil && (save.Sect == nil || save.Hero == nil) {
		err = wendao.ErrUnavailable
	}
	if err != nil {
		respondSectError(c, err)
		return
	}
	if evs == nil {
		evs = []wendao.Ev{}
	}
	view := gin.H{"sect": save.Sect.View(now), "standing": save.PathView(day), "events": evs}
	if replayed {
		view["replayed"] = true
	}
	common.ApiSuccess(c, view)
}
