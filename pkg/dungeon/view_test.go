package dungeon

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// previewBattle 是两只山魈的单场战斗，手牌换成 hand，敌人的生命、护体与状态按 enemies 摆好。
func previewBattle(t *testing.T, hand []Card, player map[string]int, enemies ...Enemy) *Run {
	t.Helper()
	r, err := NewBattle(11, BattleSetup{Class: ClassWarrior, Deck: []Card{{Id: "w_defend"}, {Id: "w_defend"}}, MaxHP: 80,
		Kind: NodeCombat, Enemies: []string{"goblin", "goblin"}, HP: 100, Dmg: 100})
	require.NoError(t, err)
	c := r.Combat
	c.Hand, c.St, c.Energy = hand, player, 3
	for i, e := range enemies {
		c.Enemies[i].HP, c.Enemies[i].Block, c.Enemies[i].St = e.HP, e.Block, e.St
	}
	return r
}

// 伤害预览与真的打出去一模一样：易伤、力量、护体、闪避、连击中途打死目标都算上；带随机伤害与不伤人的牌不预览；预览不改动
// 局面，也不用掉随机数。
func TestHandPreviewMatchesThePlay(t *testing.T) {
	cases := []struct {
		name    string
		card    Card
		player  map[string]int
		enemies []Enemy
		target  int
		want    []Hit
	}{
		{"strength, vulnerable and block", Card{Id: "w_strike"}, map[string]int{StStrength: 2},
			[]Enemy{{HP: 40, Block: 3, St: map[string]int{StVuln: 1}}, {HP: 40, St: map[string]int{}}}, 0,
			[]Hit{{HP: 9, Block: 3}, {HP: 8}}},
		{"dodge eats the first hit", Card{Id: "w_twin"}, map[string]int{},
			[]Enemy{{HP: 40, St: map[string]int{}}, {HP: 40, St: map[string]int{StDodge: 1}}}, 1,
			[]Hit{{HP: 10}, {HP: 5}}},
		{"overkill counts only the life left", Card{Id: "w_execute"}, map[string]int{},
			[]Enemy{{HP: 7, Block: 2, St: map[string]int{}}, {HP: 40, St: map[string]int{}}}, 0,
			[]Hit{{HP: 7, Block: 2}, {HP: 18}}},
		{"weak player hits every enemy", Card{Id: "w_sweep"}, map[string]int{StWeak: 1},
			[]Enemy{{HP: 40, St: map[string]int{}}, {HP: 40, Block: 10, St: map[string]int{}}}, playerTarget,
			[]Hit{{HP: 6}, {Block: 6}}},
		{"random damage is not previewed", Card{Id: "r_scatter"}, map[string]int{},
			[]Enemy{{HP: 40, St: map[string]int{}}, {HP: 40, St: map[string]int{}}}, playerTarget, nil},
		{"a skill is not previewed", Card{Id: "w_defend"}, map[string]int{},
			[]Enemy{{HP: 40, St: map[string]int{}}, {HP: 40, St: map[string]int{}}}, playerTarget, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := previewBattle(t, []Card{tc.card, {Id: "w_defend"}}, tc.player, tc.enemies...)
			before, err := common.Marshal(r)
			require.NoError(t, err)
			view := r.View()
			after, err := common.Marshal(r)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "previewing changes nothing, not even the random state")

			preview := view.Combat.Hand[0].Preview
			assert.Nil(t, view.Combat.Hand[1].Preview, "w_defend deals no damage")
			if tc.want == nil {
				assert.Nil(t, preview)
				return
			}
			// 要选目标的牌，每一项是以那个敌人为目标时的结果。
			assert.Equal(t, tc.want, preview)

			hp := []int{r.Combat.Enemies[0].HP, r.Combat.Enemies[1].HP}
			block := []int{r.Combat.Enemies[0].Block, r.Combat.Enemies[1].Block}
			require.NoError(t, r.Apply(Action{Type: "play", Node: -1, Card: 0, Target: tc.target, Choice: -1, Potion: -1}))
			for i, e := range r.Combat.Enemies {
				played := Hit{HP: hp[i] - max(e.HP, 0), Block: block[i] - e.Block}
				if tc.target == playerTarget || i == tc.target {
					assert.Equal(t, played, preview[i], "enemy %d", i)
				}
			}
		})
	}
}
