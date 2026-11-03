package wendao

import "github.com/QuantumNous/new-api/common"

// ReturnAfter 是离开多久以后才展示一次回流说明；更短的心跳照常结算，不打断玩家。
const ReturnAfter = 30 * 60

// ReturnCultivation 是这一次真正入账的主角自然修为。Kind 为 cave（普通闭关）或 vein（灵脉打坐），
// Seconds 单独按主角上次结算时间截到上限，Exp 包含自动升层消耗的修为，也按瓶颈截断。
type ReturnCultivation struct {
	Kind    string  `json:"kind"`
	Region  int     `json:"region,omitempty"`
	Seconds int64   `json:"seconds"`
	Exp     float64 `json:"exp"`
	Capped  bool    `json:"capped"`
}

// ReturnSect 是这一次真正入账的宗门资源，不含游历待领、派遣与灵田尚未领取的收获。
type ReturnSect struct {
	Seconds int64 `json:"seconds"`
	Res     Cost  `json:"res"`
	Capped  bool  `json:"capped"`
}

// ReturnReport 是一次已持久化的回归说明。Id 识别这一回，Seen 跨页面与设备记住已读；Travel 只是回来时的累计待领快照，
// 不能因为读了说明就领取。Since/At 不随之后的读取或短间隔心跳变化。
type ReturnReport struct {
	Id          string             `json:"id"`
	Since       int64              `json:"since"`
	At          int64              `json:"at"`
	CapSeconds  int64              `json:"cap_seconds"`
	Seen        bool               `json:"seen"`
	Cultivation *ReturnCultivation `json:"cultivation,omitempty"`
	Sect        *ReturnSect        `json:"sect,omitempty"`
	Travel      *TravelProgress    `json:"travel,omitempty"`
}

// Resume 是可见页面的心跳：结清自然修为与宗门产出，并在长时间离开后的第一次心跳记下说明。调用方在存档事务里执行。
// 普通 GET 只预览，不能调用它生成一份新的回流记录。旧存档没有 Seen 时，以最近一次主角或宗门结算作为保守起点。
func (sv *Save) Resume(now int64) {
	since := sv.Seen
	if since == 0 {
		if sv.Sect != nil {
			since = sv.Sect.Last
		}
		if sv.Hero != nil {
			since = max(since, sv.Hero.Last)
		}
	}
	if now < since {
		return
	}
	report := &ReturnReport{Since: since, At: now, CapSeconds: OfflineCap}
	s, h := sv.Sect, sv.Hero
	beforeRes := Cost{}
	if s != nil {
		beforeRes = s.Res
		if s.IsFounded() {
			report.Sect = &ReturnSect{Seconds: min(max(now-s.Last, 0), int64(OfflineCap)), Capped: now-s.Last >= OfflineCap}
		}
		s.Tick(now)
		if report.Sect != nil {
			report.Sect.Res = Cost{Stone: s.Res.Stone - beforeRes.Stone, Herb: s.Res.Herb - beforeRes.Herb, Ore: s.Res.Ore - beforeRes.Ore}
		}
	}
	if h != nil && s != nil {
		beforeLayer, beforeExp := h.Layer, h.Exp
		if h.Travel == nil {
			report.Cultivation = &ReturnCultivation{Kind: "cave", Seconds: min(max(now-h.Last, 0), int64(OfflineCap)), Capped: now-h.Last >= OfflineCap}
			if h.Meditation != nil {
				report.Cultivation.Kind, report.Cultivation.Region = "vein", h.Meditation.Region
			}
		}
		sv.TickHero(now)
		if report.Cultivation != nil {
			report.Cultivation.Exp = h.Exp - beforeExp
			for layer := beforeLayer; layer < h.Layer; layer++ {
				report.Cultivation.Exp += ExpNeed(h.Realm, layer)
			}
		}
		report.Travel = h.travelProgress(now)
	}
	sv.Seen = max(sv.Seen, now)
	if now-since >= ReturnAfter && (report.Cultivation != nil || report.Sect != nil || report.Travel != nil) {
		report.Id = common.GetUUID()
		sv.Back = report
	}
}

// AcknowledgeReturn 标记当前这一回已读；旧页面关闭旧提示，不会把后来新生成的一回也收起。
func (sv *Save) AcknowledgeReturn(id string) {
	if sv.Back != nil && id != "" && sv.Back.Id == id {
		sv.Back.Seen = true
	}
}
