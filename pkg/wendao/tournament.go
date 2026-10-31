package wendao

import (
	"math"
	"slices"
)

// 宗门大比：带最多三名弟子打五轮擂台，每轮派其中一名与别派弟子斗法，输一场就结束，成绩是连胜几场。对手下一招会亮出来，掌门每回合在
// 剑击、法术、护体、调息、服丹里选一招。对手的战力以出战弟子的平均战力为主、同级门派为辅，逐轮变强；
// 弟子的伤势带到下一轮，每赢一轮全员回复一成。回春丹从宗门库存里最多带三颗，没用完的结算时放回去。

const (
	TournamentRounds = 5
	// MaxTierScore 是领奖档位最多填到几场连胜。
	MaxTierScore = TournamentRounds
	// DuelTurns 是一场斗法最多几回合，到了按剩余生命的比例判胜负。
	DuelTurns = 12
	// TeamSize 是一次大比最多派几名弟子。
	TeamSize = 3
	// PouchPills 是大比最多带几颗回春丹。
	PouchPills = 3
	// DailyRewardRuns 是每天前几次大比给声望与灵石。
	DailyRewardRuns = 3

	hpPerPower  = 8
	maxQi       = 5
	spellQi     = 2
	roundRegen  = 5 // 每赢一轮全员回复最大生命的百分之几
	meditateHp  = 6
	counterMult = 0.4
	guardMult   = 0.3
	pierceMult  = 0.6 // 法术打在护体上的倍数
	chargeTaken = 1.25
)

var roundMult = [TournamentRounds]float64{0.8, 1.0, 1.15, 1.35, 1.6}

// 招式。弟子能用前五种；对手会剑击、法术、护体、蓄力，蓄力的下一回合必定是重击。
const (
	MoveStrike   = "strike"
	MoveSpell    = "spell"
	MoveGuard    = "guard"
	MoveMeditate = "meditate"
	MovePill     = "pill"
	MoveCharge   = "charge"
	MoveHeavy    = "heavy"
)

var moveDamage = map[string]float64{MoveStrike: 1, MoveSpell: 2, MoveHeavy: 2.6}

// counters 是五行相克：金克木、木克土、土克水、水克火、火克金。
var counters = [Elements]int{1, 4, 3, 0, 2}

// ElementEdge 返回 attacker 打 defender 的五行关系：1 相克(伤害 ×1.3)，-1 被克(×0.8)，0 无关。
func ElementEdge(attacker, defender int) int {
	switch {
	case counters[attacker] == defender:
		return 1
	case counters[defender] == attacker:
		return -1
	}
	return 0
}

func edgeFactor(edge int) float64 {
	switch edge {
	case 1:
		return 1.3
	case -1:
		return 0.8
	}
	return 1
}

// Fighter 是参加大比的一名弟子(Id 是弟子编号)或对手(Id 为 0，Sect 是所属门派)。
type Fighter struct {
	Id      int    `json:"id"`
	Name    [3]int `json:"name"`
	Element int    `json:"element"`
	Realm   int    `json:"realm"`
	Layer   int    `json:"layer"`
	Power   int    `json:"power"`
	Hp      int    `json:"hp"`
	MaxHp   int    `json:"max_hp"`
	Qi      int    `json:"qi"`
	Fought  int    `json:"fought"`
	Sect    int    `json:"sect"`
}

// Turn 是一回合的经过。
type Turn struct {
	Mine    string `json:"mine"`
	Theirs  string `json:"theirs"`
	Dealt   int    `json:"dealt"`
	Taken   int    `json:"taken"`
	Healed  int    `json:"healed"`
	Counter int    `json:"counter"`
	Struck  int    `json:"struck"`
}

// Tournament 是一次大比的完整状态，存在对局里。Taken 是开场从库存里拿了几颗回春丹，Pills 是还剩几颗。
type Tournament struct {
	Version  int        `json:"version"`
	Rng      Rng        `json:"rng"`
	Hall     int        `json:"hall"`
	Base     float64    `json:"base"`
	Fighters []*Fighter `json:"fighters"`
	Taken    int        `json:"taken"`
	Pills    int        `json:"pills"`
	Round    int        `json:"round"`
	Wins     int        `json:"wins"`
	Phase    string     `json:"phase"`
	Current  int        `json:"current"`
	Opponent *Fighter   `json:"opponent"`
	Intent   string     `json:"intent"`
	Charged  bool       `json:"charged"`
	Turn     int        `json:"turn"`
	Last     *Turn      `json:"last"`
	// Arena 不为空时这是一场论剑：对手是守擂宗门的弟子(满血上场)，按顺序一个个打，全部打败就赢，输一场就结束。
	Arena []*Fighter `json:"arena,omitempty"`
}

const (
	PhasePick = "pick"
	PhaseDuel = "duel"
	PhaseOver = "over"
)

// NewTournament 让宗门里的 team(一到 TeamSize 名没受伤的弟子)报名大比，带上最多三颗回春丹(调用方从库存里扣掉 Taken 颗)。
// 有弟子不在宗门或受伤时返回 ErrUnavailable。
func NewTournament(seed uint64, s *Sect, team []int, now int64) (*Tournament, error) {
	fighters, err := s.teamFighters(team, now)
	if err != nil {
		return nil, err
	}
	t := &Tournament{Version: StateVersion, Rng: Rng{S: seed}, Hall: s.Buildings[BuildingHall], Phase: PhasePick, Fighters: fighters}
	sum := 0
	for _, f := range fighters {
		sum += f.Power
	}
	// 对手主要跟着出战弟子的平均战力走，比同级门派强的宗门只占一点便宜：大比比的是临场应对，不是挂机攒的战力。
	t.Base = math.Pow(float64(sum)/float64(len(team)), 0.75) * math.Pow(HallPower(t.Hall), 0.25)
	t.Taken = min(s.Pills[PillHeal], PouchPills)
	t.Pills = t.Taken
	t.nextOpponent()
	return t, nil
}

// teamFighters 是宗门里出战的 team(一到 TeamSize 名在宗门里、没受伤的弟子)，满血上场。
func (s *Sect) teamFighters(team []int, now int64) ([]*Fighter, error) {
	if len(team) < 1 || len(team) > TeamSize {
		return nil, ErrAction
	}
	var fighters []*Fighter
	for i, id := range team {
		d := s.disciple(id)
		if d == nil || slices.Contains(team[:i], id) {
			return nil, ErrAction
		}
		if !d.home(now) {
			return nil, ErrUnavailable
		}
		fighters = append(fighters, s.fighter(d))
	}
	return fighters, nil
}

func (s *Sect) fighter(d *Disciple) *Fighter {
	power := s.Power(d)
	return &Fighter{Id: d.Id, Name: d.Name, Element: d.Element, Realm: d.Realm, Layer: d.Layer,
		Power: power, Hp: power * hpPerPower, MaxHp: power * hpPerPower}
}

// Rounds 是这一场要打几轮：大比五轮，论剑是守擂弟子的人数。
func (t *Tournament) Rounds() int {
	if len(t.Arena) > 0 {
		return len(t.Arena)
	}
	return TournamentRounds
}

func (t *Tournament) nextOpponent() {
	if len(t.Arena) > 0 {
		opponent := *t.Arena[t.Round]
		opponent.Hp, opponent.Qi = opponent.MaxHp, 0
		t.Opponent = &opponent
		t.Charged = false
		t.chooseIntent()
		return
	}
	power := max(1, int(math.Round(t.Base*roundMult[t.Round]*float64(t.Rng.Range(95, 105))/100)))
	realm := 0
	for realm < MaxRealm && realmPower[realm+1] <= float64(power) {
		realm++
	}
	layer := min(max(int(math.Round((float64(power)/realmPower[realm]-1)/0.1))+1, 1), MaxLayer)
	name := [3]int{t.Rng.Intn(NameSurnames), t.Rng.Intn(NameGivens), t.Rng.Intn(NameGivens)}
	t.Opponent = &Fighter{Name: name, Element: t.Rng.Intn(Elements), Realm: realm, Layer: layer, Power: power,
		Hp: power * hpPerPower, MaxHp: power * hpPerPower, Sect: t.Rng.Intn(OpponentSects)}
	t.Charged = false
	t.chooseIntent()
}

// chooseIntent 定下对手的下一招：蓄力之后必定重击；灵力够时会用法术，生命不多时更常护体。
func (t *Tournament) chooseIntent() {
	o := t.Opponent
	if t.Charged {
		t.Intent = MoveHeavy
		return
	}
	moves := []string{MoveStrike, MoveGuard, MoveCharge, MoveSpell}
	weights := []int{40, 20, 20, 0}
	if o.Qi >= spellQi {
		weights[3] = 30
	}
	if o.Hp*10 < o.MaxHp*3 {
		weights[1] += 15
	}
	t.Intent = moves[pickWeighted(&t.Rng, weights)]
}

// Over 表示大比结束了。
func (t *Tournament) Over() bool { return t.Phase == PhaseOver }

// Champion 表示五轮全胜(论剑是打败了全部守擂弟子)。
func (t *Tournament) Champion() bool { return t.Wins == t.Rounds() }

// Play 执行一步：pick 选第 index 名弟子上场，或者在斗法里出一招。
func (t *Tournament) Play(move string, index int) error {
	if move == "pick" {
		if t.Phase != PhasePick || index < 0 || index >= len(t.Fighters) || t.Fighters[index].Hp <= 0 {
			return ErrAction
		}
		t.Current, t.Phase, t.Turn, t.Last = index, PhaseDuel, 0, nil
		t.Fighters[index].Qi = 0
		return nil
	}
	if t.Phase != PhaseDuel {
		return ErrAction
	}
	me := t.Fighters[t.Current]
	switch move {
	case MoveStrike, MoveGuard, MoveMeditate:
	case MoveSpell:
		if me.Qi < spellQi {
			return ErrAction
		}
	case MovePill:
		if t.Pills <= 0 || me.Hp >= me.MaxHp {
			return ErrAction
		}
	default:
		return ErrAction
	}
	t.resolve(move)
	return nil
}

func (t *Tournament) hit(attacker, defender *Fighter, move string, defenderMove string) int {
	mult := moveDamage[move]
	if mult == 0 {
		return 0
	}
	damage := float64(attacker.Power) * mult * edgeFactor(ElementEdge(attacker.Element, defender.Element)) *
		float64(t.Rng.Range(90, 110)) / 100
	switch defenderMove {
	case MoveGuard:
		if move == MoveSpell {
			damage *= pierceMult
		} else {
			damage *= guardMult
		}
	case MoveCharge:
		damage *= chargeTaken
	}
	return max(1, int(math.Round(damage)))
}

// counter 是护体挡住剑击或重击时的反震伤害。
func (t *Tournament) counter(guard, attacker *Fighter, move string) int {
	if move != MoveStrike && move != MoveHeavy {
		return 0
	}
	return max(1, int(math.Round(float64(guard.Power)*counterMult*edgeFactor(ElementEdge(guard.Element, attacker.Element)))))
}

func gainQi(f *Fighter, move string) {
	switch move {
	case MoveStrike, MoveGuard, MoveCharge:
		f.Qi++
	case MoveMeditate:
		f.Qi += 2
	case MoveSpell:
		f.Qi -= spellQi
	}
	f.Qi = min(max(f.Qi, 0), maxQi)
}

func (t *Tournament) resolve(move string) {
	me, o := t.Fighters[t.Current], t.Opponent
	theirs := t.Intent
	turn := &Turn{Mine: move, Theirs: theirs}
	turn.Dealt = t.hit(me, o, move, theirs)
	turn.Taken = t.hit(o, me, theirs, move)
	if move == MoveGuard {
		turn.Counter = t.counter(me, o, theirs)
	}
	if theirs == MoveGuard {
		turn.Struck = t.counter(o, me, move)
	}
	switch move {
	case MoveMeditate:
		turn.Healed = me.MaxHp * meditateHp / 100
	case MovePill:
		turn.Healed = me.MaxHp * healPillPct / 100
		t.Pills--
	}
	gainQi(me, move)
	gainQi(o, theirs)
	t.Charged = theirs == MoveCharge
	me.Hp = min(me.Hp+turn.Healed, me.MaxHp) - turn.Taken - turn.Struck
	o.Hp -= turn.Dealt + turn.Counter
	me.Hp, o.Hp = max(me.Hp, 0), max(o.Hp, 0)
	t.Turn++
	t.Last = turn

	won, lost := false, false
	switch {
	case o.Hp <= 0:
		won = true
		me.Hp = max(me.Hp, 1)
	case me.Hp <= 0:
		lost = true
	case t.Turn >= DuelTurns:
		// 回合用完按剩余生命的比例判，一样多算输：擂台要打赢才算数。
		won = me.Hp*o.MaxHp > o.Hp*me.MaxHp
		lost = !won
	}
	switch {
	case won:
		t.Wins++
		me.Fought++
		t.Round++
		for _, f := range t.Fighters {
			if f.Hp > 0 {
				f.Hp = min(f.Hp+f.MaxHp*roundRegen/100, f.MaxHp)
			}
		}
		if t.Round == t.Rounds() {
			t.Phase = PhaseOver
			return
		}
		t.Phase = PhasePick
		t.nextOpponent()
	case lost:
		me.Fought++
		t.Phase = PhaseOver
	default:
		t.chooseIntent()
	}
}

// Score 是大比的成绩：连胜几场。
func (t *Tournament) Score() int { return t.Wins }

// TournamentReward 是一次大比给宗门的声望与灵石：每胜一场 8 声望、40×主殿等级灵石，每天只有前三次有。
func TournamentReward(t *Tournament, hall int) Cost {
	return Cost{Stone: 40 * hall * t.Wins, Rep: 8 * t.Wins}
}

// RecordTournament 把大比的结果记进宗门：放回没用完的回春丹，记战绩，今天前三次大比发声望与灵石，返回发了多少。day 是
// 游戏中心的日期。
func (s *Sect) RecordTournament(t *Tournament, day string, now int64) Cost {
	s.Pills[PillHeal] += t.Pills
	if s.Stats.Day != day {
		s.Stats.Day, s.Stats.DayRuns = day, 0
	}
	s.Stats.Tournaments++
	s.Stats.Wins += t.Wins
	s.Stats.BestRounds = max(s.Stats.BestRounds, t.Wins)
	if t.Champion() {
		s.Stats.Champions++
	}
	var reward Cost
	if s.Stats.DayRuns < DailyRewardRuns {
		reward = TournamentReward(t, s.Buildings[BuildingHall])
		s.gain(reward)
	}
	s.Stats.DayRuns++
	s.record(now, []Ev{{K: "tournament", N: t.Wins}})
	return reward
}
