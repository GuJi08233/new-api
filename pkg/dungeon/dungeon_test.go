package dungeon

import (
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRun(t *testing.T, seed uint64, class string, abyss int) *Run {
	t.Helper()
	r, err := NewRun(seed, Options{Class: class, Abyss: abyss})
	require.NoError(t, err)
	return r
}

// enterCombat 走进第一层的一场普通战。
func enterCombat(t *testing.T, r *Run) {
	t.Helper()
	require.NoError(t, r.Apply(Action{Type: "path", Node: r.Choices()[0], Card: -1, Target: -1, Choice: -1, Potion: -1}))
	require.Equal(t, PhaseCombat, r.Phase)
}

func TestAttackDamage(t *testing.T) {
	cases := []struct {
		name     string
		base     int
		attacker map[string]int
		defender map[string]int
		want     int
	}{
		{"plain", 6, nil, nil, 6},
		{"strength adds per hit", 6, map[string]int{StStrength: 2}, nil, 8},
		{"weak cuts a quarter, rounded down", 7, map[string]int{StWeak: 1}, nil, 5},
		{"vulnerable adds half, rounded down", 7, nil, map[string]int{StVuln: 1}, 10},
		{"weak applies before vulnerable", 8, map[string]int{StWeak: 1}, map[string]int{StVuln: 1}, 9},
		{"negative strength never heals", 2, map[string]int{StStrength: -5}, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, AttackDamage(tc.base, tc.attacker, tc.defender))
		})
	}
}

// 每一幕的路线图：第一层都能出发，每个节点都能从第一层走到、都能走到首领，相邻两层之间的连线不交叉，固定层的类型不变。
func TestActMapIsConnectedAndUncrossed(t *testing.T) {
	for seed := uint64(1); seed <= 300; seed++ {
		r := newTestRun(t, seed, ClassWarrior, 0)
		boss := len(r.Map) - 1
		require.Equal(t, NodeBoss, r.Map[boss].Type)
		reached := map[int]bool{}
		queue := r.Choices()
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if reached[id] {
				continue
			}
			reached[id] = true
			queue = append(queue, r.Map[id].Next...)
		}
		require.Len(t, reached, len(r.Map), "seed %d: every node is reachable", seed)
		for id, node := range r.Map[:boss] {
			require.NotEmpty(t, node.Next, "seed %d node %d leads somewhere", seed, id)
			switch node.Row {
			case 0:
				require.Equal(t, NodeCombat, node.Type)
			case 4:
				require.Equal(t, NodeTreasure, node.Type)
			case Rows - 1:
				require.Equal(t, NodeRest, node.Type)
				require.Equal(t, []int{boss}, node.Next)
			}
			if node.Row < 2 {
				require.NotEqual(t, NodeElite, node.Type)
			}
			for _, a := range node.Next {
				for otherId, other := range r.Map[:boss] {
					if other.Row != node.Row || otherId == id {
						continue
					}
					for _, b := range other.Next {
						crossed := (node.Col < other.Col && r.Map[a].Col > r.Map[b].Col) || (node.Col > other.Col && r.Map[a].Col < r.Map[b].Col)
						require.False(t, crossed, "seed %d: edges from %d and %d cross", seed, id, otherId)
					}
				}
			}
		}
	}
}

// 灼烧在敌人回合开始时结算、无视护甲，然后减一层。
func TestBurnIgnoresBlockAndDecays(t *testing.T) {
	r := newTestRun(t, 7, ClassMage, 0)
	enterCombat(t, r)
	e := r.Combat.Enemies[0]
	e.HP, e.MaxHP, e.Block = 40, 40, 0
	e.St = map[string]int{StBurn: 3}
	require.NoError(t, r.Apply(Action{Type: "end", Node: -1, Card: -1, Target: -1, Choice: -1, Potion: -1}))
	assert.Equal(t, 37, e.HP)
	assert.Equal(t, 2, e.St[StBurn])
}

func TestPlayCardRejectsIllegalPlays(t *testing.T) {
	r := newTestRun(t, 11, ClassWarrior, 0)
	enterCombat(t, r)
	c := r.Combat
	c.Hand = []Card{{Id: "w_strike"}, {Id: DoubtCard}, {Id: "w_execute"}}
	c.Energy = 1
	play := func(card, target int) error {
		return r.Apply(Action{Type: "play", Node: -1, Card: card, Target: target, Choice: -1, Potion: -1})
	}
	assert.ErrorIs(t, play(1, 0), ErrAction, "curses cannot be played")
	assert.ErrorIs(t, play(2, 0), ErrAction, "not enough energy")
	assert.ErrorIs(t, play(0, len(c.Enemies)), ErrAction, "target must be an enemy")
	c.Enemies[0].Dead = true
	if len(c.Enemies) == 1 {
		c.Enemies = append(c.Enemies, &Enemy{Id: "goblin", HP: 20, MaxHP: 20, St: map[string]int{}})
	}
	assert.ErrorIs(t, play(0, 0), ErrAction, "dead enemies cannot be targeted")
	require.NoError(t, play(0, 1))
	assert.Equal(t, 0, c.Energy)
}

// 存档往返之后接着玩，与不存档一直玩的结果完全相同：随机数状态随存档保存。
func TestSavedRunContinuesIdentically(t *testing.T) {
	r := newTestRun(t, 42, ClassRanger, 3)
	enterCombat(t, r)
	data, err := common.Marshal(r)
	require.NoError(t, err)
	var restored Run
	require.NoError(t, common.Unmarshal(data, &restored))
	end := Action{Type: "end", Node: -1, Card: -1, Target: -1, Choice: -1, Potion: -1}
	for range 3 {
		errA, errB := r.Apply(end), restored.Apply(end)
		require.Equal(t, errA, errB)
	}
	a, err := common.Marshal(r)
	require.NoError(t, err)
	b, err := common.Marshal(&restored)
	require.NoError(t, err)
	assert.JSONEq(t, string(a), string(b))
}

// 页面看不到随机数状态，抽牌堆按牌名排序而不是抽牌顺序。
func TestViewHidesDrawOrderAndRng(t *testing.T) {
	r := newTestRun(t, 5, ClassWarrior, 0)
	enterCombat(t, r)
	data, err := common.Marshal(r.View())
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"rng"`)
	view := r.View()
	ids := make([]string, 0, len(view.Combat.Draw))
	for _, card := range view.Combat.Draw {
		ids = append(ids, card.Id)
	}
	assert.True(t, slices.IsSorted(ids))
	assert.Len(t, view.Combat.Draw, len(r.Combat.Draw))
}

func TestAbyssModifiersAtStart(t *testing.T) {
	cases := []struct {
		abyss int
		maxHP int
		doubt bool
	}{
		{0, 84, false},
		{4, 84, false},
		{5, 79, false},
		{6, 79, true},
		{10, 79, true},
	}
	for _, tc := range cases {
		r := newTestRun(t, 1, ClassWarrior, tc.abyss)
		assert.Equal(t, tc.maxHP, r.MaxHP, "abyss %d", tc.abyss)
		assert.Equal(t, tc.doubt, slices.ContainsFunc(r.Deck, func(c Card) bool { return c.Id == DoubtCard }), "abyss %d", tc.abyss)
	}
	_, err := NewRun(1, Options{Class: ClassWarrior, Abyss: MaxAbyss + 1})
	assert.ErrorIs(t, err, ErrAction)
}

// 无尽远征的成绩是打通的幕数，魂晶按到达的层数、精英与首领计算，深渊等级每级加 10%。
func TestScoreAndCrystals(t *testing.T) {
	r := newTestRun(t, 1, ClassWarrior, 2)
	r.Floor, r.Elites, r.Bosses = 12, 2, 1
	assert.Equal(t, 1, r.Score())
	assert.Equal(t, (12+6+10)*12/10, r.Crystals())
	r.Floor, r.Bosses = 40, 5
	assert.Equal(t, 5, r.Score())
	assert.Equal(t, (40+6+50)*12/10, r.Crystals())
}

func TestActScale(t *testing.T) {
	cases := []struct {
		act, source int
		hp, dmg     int
	}{
		{1, 1, 100, 100},
		{3, 3, 100, 100},
		{2, 3, 100, 100},
		{2, 1, 150, 130},
		{3, 1, 220, 170},
		{4, 3, 125, 112},
		{4, 1, 275, 190},
		{7, 3, 200, 148},
	}
	for _, tc := range cases {
		hp, dmg := ActScale(tc.act, tc.source)
		assert.Equal(t, [2]int{tc.hp, tc.dmg}, [2]int{hp, dmg}, "act %d from act %d", tc.act, tc.source)
	}
}

// 打败第三幕首领之后进入第四幕：首领轮换回第一幕的首领，按无尽深渊的倍率放大。
func TestEndlessActsScaleEnemies(t *testing.T) {
	r := newTestRun(t, 3, ClassWarrior, 0)
	r.Act = Acts + 1
	r.newAct()
	boss := len(r.Map) - 1
	r.Path = []int{boss - 1}
	r.Map[boss-1].Next = []int{boss}
	require.NoError(t, r.Apply(Action{Type: "path", Node: boss, Card: -1, Target: -1, Choice: -1, Potion: -1}))
	require.Equal(t, PhaseCombat, r.Phase)
	e := r.Combat.Enemies[0]
	hp, dmg := ActScale(Acts+1, 1)
	assert.Equal(t, "slime_king", e.Id)
	assert.Equal(t, 140*hp/100, e.MaxHP)
	assert.Equal(t, dmg-100, e.DmgPct)
}

// 打通前三幕解锁下一个深渊等级并发首通魂晶，同一等级再打通不再发首通魂晶。
func TestMetaRecordUnlocksNextAbyssOnce(t *testing.T) {
	meta, err := ParseMeta("")
	require.NoError(t, err)
	_, err = meta.Options(ClassWarrior, 1)
	assert.ErrorIs(t, err, ErrLocked)
	_, err = meta.Options(ClassMage, 0)
	assert.ErrorIs(t, err, ErrLocked, "mage starts locked")

	r := newTestRun(t, 1, ClassWarrior, 0)
	r.Floor, r.Bosses, r.Phase = 2, 0, PhaseLost
	assert.Equal(t, RunRecord{Crystals: 2, Floor: 2}, meta.Record(r))
	assert.Equal(t, 0, meta.MaxAbyss(ClassWarrior))

	r.Floor, r.Bosses = 30, 3
	first := meta.Record(r)
	assert.Equal(t, RunRecord{Crystals: r.Crystals() + FirstClearBonus(0), Bonus: FirstClearBonus(0), Floor: 30, Acts: 3}, first)
	assert.Equal(t, 1, meta.MaxAbyss(ClassWarrior))
	second := meta.Record(r)
	assert.Equal(t, 0, second.Bonus)
	assert.Equal(t, 2+first.Crystals+second.Crystals, meta.Crystals)
	assert.Equal(t, 2, meta.Records[ClassWarrior].Wins)
	assert.Equal(t, 3, meta.Records[ClassWarrior].BestActs)

	encoded, err := meta.Encode()
	require.NoError(t, err)
	again, err := ParseMeta(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, again)
}

func TestMetaBuy(t *testing.T) {
	meta, err := ParseMeta("")
	require.NoError(t, err)
	meta.Crystals = 60
	require.NoError(t, meta.Buy("class:"+ClassMage))
	assert.Equal(t, 20, meta.Crystals)
	assert.ErrorIs(t, meta.Buy("class:"+ClassMage), ErrMaxed)
	assert.ErrorIs(t, meta.Buy("class:"+ClassRanger), ErrCrystals)
	require.NoError(t, meta.Buy("talent:"+TalentVitality))
	assert.Equal(t, 0, meta.Crystals)
	assert.Equal(t, 1, meta.Talents[TalentVitality])
	assert.ErrorIs(t, meta.Buy("talent:nope"), ErrAction)
	assert.ErrorIs(t, meta.Buy("relic:"+RelicCrown), ErrAction)

	meta.Crystals = 1000
	for range 2 {
		require.NoError(t, meta.Buy("talent:"+TalentVitality))
	}
	assert.ErrorIs(t, meta.Buy("talent:"+TalentVitality), ErrMaxed)
	options, err := meta.Options(ClassMage, 0)
	require.NoError(t, err)
	r, err := NewRun(1, options)
	require.NoError(t, err)
	assert.Equal(t, classStats[ClassMage].HP+12, r.MaxHP)
}

// 每张会出现在奖励里的牌、每个敌人的每一招都有内容，敌人的循环与权重都指向存在的招式。
func TestContentIsWellFormed(t *testing.T) {
	for _, d := range cardList {
		if d.Type == TypeCurse {
			continue
		}
		assert.NotEmpty(t, d.fx, d.Id)
		for _, f := range d.fx {
			if strings.HasPrefix(f.kind, "apply") || f.kind == FxPower || f.kind == FxAddCard {
				assert.NotEmpty(t, f.status, d.Id)
			}
		}
	}
	for _, d := range enemyDefs {
		if len(d.Cycle) > 0 {
			for _, i := range d.Cycle {
				assert.Less(t, i, len(d.Moves), d.Id)
			}
		} else {
			assert.Len(t, d.Weights, len(d.Moves), d.Id)
		}
	}
	for act, enc := range actEncounters {
		for _, group := range append(append(slices.Clone(enc.Normal), enc.Elite...), enc.Boss) {
			for _, id := range group {
				assert.NotNil(t, enemyDefs[id], "act %d enemy %s", act+1, id)
			}
		}
	}
}

// 历练专有的敌人不进九幽秘境的幕；练习的敌人表里有它们，没有出身的幕、标着出自历练第几章(页面靠它与训练木桩区分)；
// 单场战斗与练习都认得它们。
func TestJourneyEnemiesStayOutOfTheActs(t *testing.T) {
	inCatalog := map[string]CatalogEnemy{}
	for _, e := range NewCatalog().Enemies {
		inCatalog[e.Id] = e
	}
	assert.Zero(t, inCatalog[DummyEnemy].Chapter)
	for _, d := range journeyEnemyList {
		assert.Zero(t, NativeAct(d.Id), d.Id)
		entry := inCatalog[d.Id]
		assert.Zero(t, entry.Act, d.Id)
		assert.Contains(t, []int{4, 5}, entry.Chapter, d.Id)
		assert.Contains(t, []string{NodeCombat, NodeElite, NodeBoss}, entry.Kind, d.Id)
		_, err := NewPractice(1, PracticeSetup{Class: ClassMage, Deck: StarterDeck(ClassMage), Enemies: []string{d.Id}, Stage: 1})
		assert.NoError(t, err, d.Id)
		_, err = NewBattle(1, BattleSetup{Class: ClassWarrior, Deck: StarterDeck(ClassWarrior), MaxHP: 100, Kind: NodeCombat,
			Enemies: []string{d.Id}, HP: 100, Dmg: 100})
		assert.NoError(t, err, d.Id)
	}
}

// 试炼的星级：过关一星，回合数达标一星，剩余生命达标一星；存档只记最高星级，新拿到的星才发魂晶，过关才解锁下一关。
func TestTrialStarsAndRecord(t *testing.T) {
	def := Trials[0]
	r, err := NewTrial(1, def.Id)
	require.NoError(t, err)
	require.Equal(t, PhaseCombat, r.Phase)
	assert.Equal(t, 0, r.Stars())

	r.Phase, r.Turns, r.HP = PhaseWon, def.Turns+1, r.MaxHP*def.HPPct/100-1
	assert.Equal(t, 1, r.Stars())
	meta, err := ParseMeta("")
	require.NoError(t, err)
	assert.False(t, meta.TrialUnlocked(Trials[1].Id))
	assert.Equal(t, RunRecord{Stars: 1, Crystals: def.Crystals}, meta.RecordTrial(r))
	assert.True(t, meta.TrialUnlocked(Trials[1].Id))
	assert.False(t, meta.TrialUnlocked(Trials[2].Id))

	r.Turns, r.HP = def.Turns, r.MaxHP
	assert.Equal(t, 3, r.Stars())
	assert.Equal(t, RunRecord{Stars: 3, Crystals: 2 * def.Crystals}, meta.RecordTrial(r))
	assert.Equal(t, RunRecord{Stars: 3}, meta.RecordTrial(r), "stars already earned pay nothing")
	assert.Equal(t, 3*def.Crystals, meta.Crystals)
}

// 试炼的几场战斗连着打，生命不回复，回合数累加。
func TestTrialFightsCarryOver(t *testing.T) {
	index := slices.IndexFunc(Trials, func(d TrialDef) bool { return len(d.Fights) > 1 })
	require.GreaterOrEqual(t, index, 0)
	r, err := NewTrial(9, Trials[index].Id)
	require.NoError(t, err)
	r.HP = 20
	r.Combat.Turn = 3
	for _, e := range r.Combat.Enemies {
		e.HP = 1
	}
	r.Combat.Hand = []Card{{Id: "m_meteor", Up: true}}
	r.Combat.Energy = 3
	require.NoError(t, r.Apply(Action{Type: "play", Node: -1, Card: 0, Target: -1, Choice: -1, Potion: -1}))
	assert.Equal(t, PhaseCombat, r.Phase)
	assert.Equal(t, 1, r.Fight)
	assert.Equal(t, 3, r.Turns)
	assert.Equal(t, 20, r.HP)
}

func TestPracticeSetupAndReset(t *testing.T) {
	valid := PracticeSetup{Class: ClassMage, Deck: []Card{{Id: "m_bolt"}, {Id: "m_meteor", Up: true}}, Relics: []string{RelicWhetstone},
		Enemies: []string{DummyEnemy}, Stage: 1, InfiniteEnergy: true, Invincible: true}
	invalid := []PracticeSetup{
		{Class: "bard", Deck: valid.Deck, Enemies: valid.Enemies, Stage: 1},
		{Class: ClassMage, Enemies: valid.Enemies, Stage: 1},
		{Class: ClassMage, Deck: valid.Deck, Enemies: []string{"slime", "slime", "slime", "slime"}, Stage: 1},
		{Class: ClassMage, Deck: valid.Deck, Enemies: valid.Enemies, Stage: 0},
		{Class: ClassMage, Deck: valid.Deck, Enemies: valid.Enemies, Stage: 1, Relics: []string{RelicCrown, RelicCrown}},
		{Class: ClassMage, Deck: []Card{{Id: DoubtCard, Up: true}}, Enemies: valid.Enemies, Stage: 1},
	}
	for i, s := range invalid {
		_, err := NewPractice(1, s)
		assert.ErrorIs(t, err, ErrAction, "setup %d", i)
	}

	r, err := NewPractice(1, valid)
	require.NoError(t, err)
	assert.Equal(t, classStats[ClassMage].HP, r.MaxHP)
	assert.Equal(t, 999, r.Combat.Enemies[0].HP)
	r.Combat.Hand = []Card{{Id: "m_meteor", Up: true}}
	r.Combat.Energy = 0
	require.NoError(t, r.Apply(Action{Type: "play", Node: -1, Card: 0, Target: -1, Choice: -1, Potion: -1}), "infinite energy ignores cost")
	assert.Equal(t, 26+1, r.Combat.Dealt, "whetstone adds one strength")
	assert.Equal(t, 27, r.Combat.MaxHit)

	r.loseHP(500, "")
	assert.Equal(t, 1, r.HP, "invincible stops at 1")
	assert.Equal(t, PhaseCombat, r.Phase)
	require.NoError(t, r.Apply(Action{Type: "reset", Node: -1, Card: -1, Target: -1, Choice: -1, Potion: -1}))
	assert.Equal(t, r.MaxHP, r.HP)
	assert.Equal(t, 0, r.Combat.Dealt)

	scaled, err := NewPractice(1, PracticeSetup{Class: ClassWarrior, Deck: valid.Deck, Enemies: []string{"slime_king"}, Stage: 3})
	require.NoError(t, err)
	hp, _ := ActScale(3, 1)
	assert.Equal(t, 140*hp/100, scaled.Combat.Enemies[0].MaxHP)
	assert.False(t, scaled.Over(), "practice never settles")
}

// 护命符挂在身上，倒下时在不死鸟羽毛之后生效，以一半生命复活，每轮只救一次；用掉之后不能再挂第二张。
func TestWardRevivesOnceAfterThePhoenix(t *testing.T) {
	r := newTestRun(t, 7, ClassWarrior, 0)
	enterCombat(t, r)
	r.Relics = append(r.Relics, RelicPhoenix)
	require.NoError(t, r.UseItem(ItemWard))
	assert.ErrorIs(t, r.UseItem(ItemWard), ErrItemLimit, "one ward at a time")

	r.loseHP(r.HP, "test")
	assert.True(t, r.PhoenixUsed)
	assert.True(t, r.Ward, "the phoenix feather saves first")
	assert.Equal(t, r.MaxHP*30/100, r.HP)

	r.loseHP(r.HP, "test")
	assert.False(t, r.Ward)
	assert.True(t, r.WardUsed)
	assert.Equal(t, r.MaxHP*50/100, r.HP)
	assert.ErrorIs(t, r.UseItem(ItemWard), ErrItemLimit, "a run is saved by at most one ward")

	r.loseHP(r.HP, "test")
	assert.Equal(t, PhaseLost, r.Phase)
	assert.ErrorIs(t, r.UseItem(ItemGold), ErrAction, "items cannot be used once the run is over")
}

// 每轮最多用 ItemsPerRun 件道具；用了没有效果的道具(满血喝药剂、药水栏已满)不算次数。
func TestItemsPerRunAndUselessItems(t *testing.T) {
	r := newTestRun(t, 3, ClassWarrior, 0)
	assert.ErrorIs(t, r.UseItem(ItemElixir), ErrItemLimit)
	for len(r.Potions) < r.PotionSlots() {
		require.NoError(t, r.UseItem(ItemPotion))
	}
	assert.ErrorIs(t, r.UseItem(ItemPotion), ErrItemLimit)
	potions := r.ItemsUsed
	assert.Equal(t, r.PotionSlots(), potions)

	relics := len(r.Relics)
	for r.ItemsUsed < ItemsPerRun {
		require.NoError(t, r.UseItem(ItemRelic))
	}
	assert.Len(t, r.Relics, relics+ItemsPerRun-potions)
	assert.ErrorIs(t, r.UseItem(ItemGold), ErrItemLimit)
	assert.ErrorIs(t, r.UseItem("crown"), ErrAction)
}

// 背包每种道具最多 MaxItemStack 件，拿完就从背包里去掉。
func TestMetaItems(t *testing.T) {
	m, err := ParseMeta("")
	require.NoError(t, err)
	require.NoError(t, m.AddItem(ItemWard, MaxItemStack))
	assert.ErrorIs(t, m.AddItem(ItemWard, 1), ErrItemLimit)
	assert.ErrorIs(t, m.AddItem("crown", 1), ErrAction)
	assert.ErrorIs(t, m.TakeItem(ItemGold), ErrNoItem)
	for range MaxItemStack {
		require.NoError(t, m.TakeItem(ItemWard))
	}
	assert.Empty(t, m.Items)
}

// 单场战斗：开战时带上法宝、丹符与境界给的力量、护甲；赢了以后从几张功法里选一张收下再结束，输了直接结束，都不能再操作。
func TestBattleRewardsOneCardAndEnds(t *testing.T) {
	setup := BattleSetup{Class: ClassWarrior, Deck: []Card{{Id: "w_strike"}, {Id: "w_defend"}}, Relics: []string{RelicWhetstone},
		Potions: []string{PotionFire}, MaxHP: 90, Strength: 2, Block: 12, Kind: NodeElite, Enemies: []string{"goblin", "goblin"}, HP: 150, Dmg: 100,
		Rewards: 3}
	invalid := []BattleSetup{
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 90, Kind: "shop", Enemies: setup.Enemies, HP: 100, Dmg: 100},
		{Class: ClassWarrior, Deck: []Card{{Id: KnifeCard}}, MaxHP: 90, Kind: NodeCombat, Enemies: setup.Enemies, HP: 100, Dmg: 100},
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 90, Kind: NodeCombat, Enemies: []string{DummyEnemy}, HP: 100, Dmg: 100},
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 90, Kind: NodeCombat, Enemies: setup.Enemies, HP: 10, Dmg: 100},
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 0, Kind: NodeCombat, Enemies: setup.Enemies, HP: 100, Dmg: 100},
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 90, Strength: BattleMaxStrength + 1, Kind: NodeCombat, Enemies: setup.Enemies,
			HP: 100, Dmg: 100},
		{Class: ClassWarrior, Deck: setup.Deck, MaxHP: 90, Block: BattleMaxBlock + 1, Kind: NodeCombat, Enemies: setup.Enemies, HP: 100,
			Dmg: 100},
	}
	for i, s := range invalid {
		_, err := NewBattle(1, s)
		assert.ErrorIs(t, err, ErrAction, "setup %d", i)
	}

	r, err := NewBattle(3, setup)
	require.NoError(t, err)
	assert.Equal(t, 90, r.HP)
	assert.Equal(t, []string{PotionFire}, r.Potions)
	assert.Equal(t, 2+1, r.Combat.St[StStrength], "realm strength plus the whetstone")
	assert.Equal(t, 12, r.Combat.Block, "block from the setup lasts through the first turn")
	for _, e := range r.Combat.Enemies {
		assert.GreaterOrEqual(t, e.MaxHP, 18*150/100)
		e.HP = 1
	}
	none := Action{Node: -1, Card: -1, Target: -1, Choice: -1, Potion: -1}
	r.Combat.Hand = []Card{{Id: "w_strike"}, {Id: "w_strike"}}
	r.Combat.Energy = 3
	play := none
	play.Type, play.Card, play.Target = "play", 0, 0
	require.NoError(t, r.Apply(play))
	play.Target = 1
	require.NoError(t, r.Apply(play))
	assert.Equal(t, PhaseReward, r.Phase)
	assert.Equal(t, PhaseWon, r.Result)
	assert.Len(t, r.Reward.Cards, 3)
	assert.False(t, r.Finished())

	pick := none
	pick.Type, pick.Choice = "reward", 1
	want := r.Reward.Cards[1]
	require.NoError(t, r.Apply(pick))
	assert.True(t, r.Finished())
	assert.Equal(t, &want, r.Picked)
	assert.ErrorIs(t, r.Apply(play), ErrAction, "a finished battle takes no more actions")

	lost, err := NewBattle(4, setup)
	require.NoError(t, err)
	lost.loseHP(lost.HP, "test")
	assert.True(t, lost.Finished())
	assert.Equal(t, PhaseLost, lost.Result)
	assert.Nil(t, lost.Picked)
}
