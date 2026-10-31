package dungeon

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	// ErrLocked 表示职业还没解锁，或者深渊等级超过了这个职业已经解锁的等级。
	ErrLocked = errors.New("dungeon class or abyss level is locked")
	// ErrCrystals 表示魂晶不够。
	ErrCrystals = errors.New("not enough soul crystals")
	// ErrMaxed 表示已经解锁或者天赋已经满级。
	ErrMaxed = errors.New("already unlocked")
)

// ClassCosts 是解锁各职业要花的魂晶，剑士一开始就有。
var ClassCosts = map[string]int{ClassWarrior: 0, ClassMage: 40, ClassRanger: 80}

// TalentDef 是一种天赋：Costs[i] 是升到第 i+1 级要花的魂晶。
type TalentDef struct {
	Id    string `json:"id"`
	Costs []int  `json:"costs"`
}

// Talents 是所有天赋，按页面上的排列顺序。
var Talents = []TalentDef{
	{Id: TalentVitality, Costs: []int{20, 40, 60}},
	{Id: TalentPurse, Costs: []int{15, 30}},
	{Id: TalentDrill, Costs: []int{30, 60}},
	{Id: TalentPotion, Costs: []int{40}},
	{Id: TalentMedic, Costs: []int{40}},
	{Id: TalentHaggle, Costs: []int{50}},
	{Id: TalentRelic, Costs: []int{80}},
}

// ClassRecord 是一个职业在无尽远征里的战绩。Wins 是打通前三幕的次数；Cleared 是打通过前三幕的最高深渊等级加一，0 表示
// 还没打通过，能选的深渊等级最高为 Cleared；BestActs 是一轮最多打通了几幕。
type ClassRecord struct {
	Runs      int `json:"runs"`
	Wins      int `json:"wins"`
	Cleared   int `json:"cleared"`
	BestFloor int `json:"best_floor"`
	BestActs  int `json:"best_acts"`
}

// Meta 是玩家在深渊远征里的局外存档：魂晶、解锁的职业、天赋与各职业战绩。
type Meta struct {
	Crystals int                     `json:"crystals"`
	Earned   int                     `json:"earned"`
	Runs     int                     `json:"runs"`
	Wins     int                     `json:"wins"`
	Classes  []string                `json:"classes"`
	Talents  map[string]int          `json:"talents"`
	Records  map[string]*ClassRecord `json:"records"`
	// Trials 是每关试炼拿到过的最高星级。
	Trials map[string]int `json:"trials"`
}

// ParseMeta 读出存档，空字符串是一份新存档。
func ParseMeta(data string) (*Meta, error) {
	m := &Meta{}
	if strings.TrimSpace(data) != "" {
		if err := common.UnmarshalJsonStr(data, m); err != nil {
			return nil, err
		}
	}
	if m.Talents == nil {
		m.Talents = map[string]int{}
	}
	if m.Records == nil {
		m.Records = map[string]*ClassRecord{}
	}
	if m.Trials == nil {
		m.Trials = map[string]int{}
	}
	if !slices.Contains(m.Classes, ClassWarrior) {
		m.Classes = append([]string{ClassWarrior}, m.Classes...)
	}
	for _, class := range Classes {
		if m.Records[class] == nil {
			m.Records[class] = &ClassRecord{}
		}
	}
	return m, nil
}

// Encode 把存档序列化。
func (m *Meta) Encode() (string, error) {
	data, err := common.Marshal(m)
	return string(data), err
}

// MaxAbyss 是这个职业现在能选的最高深渊等级。
func (m *Meta) MaxAbyss(class string) int {
	return min(m.Records[class].Cleared, MaxAbyss)
}

// Options 校验开局的选择并带上天赋。
func (m *Meta) Options(class string, abyss int) (Options, error) {
	if _, ok := classStats[class]; !ok || !slices.Contains(m.Classes, class) || abyss < 0 || abyss > m.MaxAbyss(class) {
		return Options{}, ErrLocked
	}
	return Options{Class: class, Abyss: abyss, Talents: m.Talents}, nil
}

// Buy 花魂晶解锁一个职业(class:<id>)或把一种天赋升一级(talent:<id>)。
func (m *Meta) Buy(item string) error {
	kind, id, _ := strings.Cut(item, ":")
	cost := 0
	switch kind {
	case "class":
		price, ok := ClassCosts[id]
		if !ok {
			return ErrAction
		}
		if slices.Contains(m.Classes, id) {
			return ErrMaxed
		}
		cost = price
	case "talent":
		index := slices.IndexFunc(Talents, func(t TalentDef) bool { return t.Id == id })
		if index < 0 {
			return ErrAction
		}
		level := m.Talents[id]
		if level >= len(Talents[index].Costs) {
			return ErrMaxed
		}
		cost = Talents[index].Costs[level]
	default:
		return ErrAction
	}
	if m.Crystals < cost {
		return ErrCrystals
	}
	m.Crystals -= cost
	if kind == "class" {
		m.Classes = append(m.Classes, id)
	} else {
		m.Talents[id]++
	}
	return nil
}

// RunRecord 是一轮记进存档的结果：Crystals 含 Bonus。无尽远征的 Bonus 是第一次在这个深渊等级打通前三幕的额外魂晶；
// 试炼的 Stars 是这一次的星级，Crystals 是新拿到的星的奖励。
type RunRecord struct {
	Crystals int `json:"crystals"`
	Bonus    int `json:"bonus,omitempty"`
	Floor    int `json:"floor,omitempty"`
	Acts     int `json:"acts,omitempty"`
	Stars    int `json:"stars,omitempty"`
}

// FirstClearBonus 是某个职业第一次在深渊等级 abyss 打通前三幕的额外魂晶。
func FirstClearBonus(abyss int) int { return 15 + 5*abyss }

// Record 把结束(倒下或放弃)的一轮无尽远征记进存档：发魂晶、记战绩；打通前三幕时解锁这个职业的下一个深渊等级。
func (m *Meta) Record(r *Run) RunRecord {
	rec := m.Records[r.Class]
	if rec == nil {
		rec = &ClassRecord{}
		m.Records[r.Class] = rec
	}
	result := RunRecord{Crystals: r.Crystals(), Floor: r.Floor, Acts: r.Bosses}
	rec.Runs++
	m.Runs++
	rec.BestFloor = max(rec.BestFloor, r.Floor)
	rec.BestActs = max(rec.BestActs, r.Bosses)
	if r.Bosses >= Acts {
		rec.Wins++
		m.Wins++
		if r.Abyss+1 > rec.Cleared {
			result.Bonus = FirstClearBonus(r.Abyss)
			rec.Cleared = r.Abyss + 1
		}
	}
	result.Crystals += result.Bonus
	m.Crystals += result.Crystals
	m.Earned += result.Crystals
	return result
}

// TrialUnlocked 表示这一关试炼可以挑战：第一关总是可以，之后要上一关过关。
func (m *Meta) TrialUnlocked(id string) bool {
	index := TrialIndex(id)
	return index == 0 || (index > 0 && m.Trials[Trials[index-1].Id] > 0)
}

// RecordTrial 把结束的一关试炼记进存档：星级只记最高的，新拿到的每颗星发一次魂晶。
func (m *Meta) RecordTrial(r *Run) RunRecord {
	stars := r.Stars()
	result := RunRecord{Stars: stars}
	index := TrialIndex(r.Trial)
	if index < 0 {
		return result
	}
	if gained := stars - m.Trials[r.Trial]; gained > 0 {
		result.Crystals = gained * Trials[index].Crystals
		m.Trials[r.Trial] = stars
		m.Crystals += result.Crystals
		m.Earned += result.Crystals
	}
	return result
}
