package sect

import "slices"

// 论剑：掌门之间的异步对战。每个宗门设一套守擂阵容(最多 TeamSize 名弟子)，别的掌门带自己的弟子来挑战，规则与大比的斗法相同：
// 守擂弟子按顺序满血上场、由电脑按亮出的招式出手，挑战方依次打败全部守擂弟子就赢，输一场就结束；回春丹同样从库存里带。
// 守擂阵容只记弟子编号，被挑战时按守擂宗门当时的实力上场(弟子在外历练或受伤也照常守擂)。论剑只发宗门资源，不发游戏币。

const (
	// ArenaDailyRuns 是每天前几次论剑有声望与灵石。之后照常能挑战、照常计积分，只是没有资源。
	ArenaDailyRuns = 5
)

// ArenaStats 是宗门的论剑战绩，Day 与 DayRuns 记今天挑战了几次。
type ArenaStats struct {
	Battles int    `json:"battles"`
	Wins    int    `json:"wins"`
	Day     string `json:"day"`
	DayRuns int    `json:"day_runs"`
}

// ArenaLineup 校验并整理守擂阵容：一到 TeamSize 名宗门里的弟子，不能重复。弟子在不在宗门里不要紧。
func (s *Sect) ArenaLineup(ids []int) ([]int, error) {
	if len(ids) < 1 || len(ids) > TeamSize {
		return nil, ErrAction
	}
	for i, id := range ids {
		if s.disciple(id) == nil || slices.Contains(ids[:i], id) {
			return nil, ErrAction
		}
	}
	return slices.Clone(ids), nil
}

// ArenaDefenders 是守擂阵容按宗门现在的实力上场的样子。已经离开宗门的弟子跳过；一个都不剩时返回空。
func (s *Sect) ArenaDefenders(ids []int) []*Fighter {
	var fighters []*Fighter
	for _, id := range ids {
		if d := s.disciple(id); d != nil {
			f := s.fighter(d)
			f.Sect = -1
			fighters = append(fighters, f)
		}
	}
	return fighters
}

// ArenaPower 是守擂阵容的总战力。
func ArenaPower(defenders []*Fighter) int {
	sum := 0
	for _, f := range defenders {
		sum += f.Power
	}
	return sum
}

// NewArenaBattle 让宗门里的 team 挑战 defenders(守擂宗门的 ArenaDefenders)，带上最多三颗回春丹(调用方从库存里扣掉 Taken 颗)。
func NewArenaBattle(seed uint64, s *Sect, team []int, defenders []*Fighter, now int64) (*Tournament, error) {
	if len(defenders) < 1 || len(defenders) > TeamSize {
		return nil, ErrAction
	}
	fighters, err := s.teamFighters(team, now)
	if err != nil {
		return nil, err
	}
	t := &Tournament{Version: StateVersion, Rng: Rng{S: seed}, Hall: s.Buildings[BuildingHall], Phase: PhasePick,
		Fighters: fighters, Arena: defenders}
	t.Taken = min(s.Pills[PillHeal], PouchPills)
	t.Pills = t.Taken
	t.nextOpponent()
	return t, nil
}

// ArenaReward 是一场论剑给宗门的声望与灵石：每打败一名守擂弟子 4 声望、20×主殿等级灵石，全部打败另加 8 声望。
func ArenaReward(t *Tournament, hall int) Cost {
	reward := Cost{Stone: 20 * hall * t.Wins, Rep: 4 * t.Wins}
	if t.Champion() {
		reward.Rep += 8
	}
	return reward
}

// RecordArena 把论剑的结果记进宗门：放回没用完的回春丹，记战绩，今天前 ArenaDailyRuns 次论剑发声望与灵石，返回发了多少。
func (s *Sect) RecordArena(t *Tournament, day string, now int64) Cost {
	s.Pills[PillHeal] += t.Pills
	if s.Arena.Day != day {
		s.Arena.Day, s.Arena.DayRuns = day, 0
	}
	s.Arena.Battles++
	if t.Champion() {
		s.Arena.Wins++
	}
	var reward Cost
	if s.Arena.DayRuns < ArenaDailyRuns {
		reward = ArenaReward(t, s.Buildings[BuildingHall])
		s.gain(reward)
	}
	s.Arena.DayRuns++
	n := 0
	if t.Champion() {
		n = 1
	}
	s.record(now, []Ev{{K: "arena", N: n}})
	return reward
}
