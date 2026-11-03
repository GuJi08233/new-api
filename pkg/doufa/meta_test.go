package doufa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 天赋在开局时带进来：生命上限、加成、退路、静修的回复和开局的祝福都按等级折算，超过满级的按满级算，不认识的天赋忽略。
func TestTalentsShapeTheOpening(t *testing.T) {
	plain, err := NewRun(7, "warrior", nil)
	require.NoError(t, err)
	assert.Equal(t, BaseRetreats, plain.Retreats)
	assert.Empty(t, plain.Blessings)
	assert.Equal(t, content.Classes["warrior"].HP, plain.HP)

	run, err := NewRun(7, "warrior", map[string]int{"vitality": 2, "might": 9, "anchor": 1, "fortune": 1, "nonsense": 4})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"vitality": 2, "might": 3, "anchor": 1, "fortune": 1}, run.Talents, "levels are capped, unknown talents dropped")
	assert.Equal(t, BaseRetreats+1, run.Retreats)
	require.Len(t, run.Blessings, 1, "the fortune talent starts the run with a blessing")
	blessing := run.Blessings[0]
	want := Mods{Damage: 12, Vigor: 16}
	for _, b := range content.Blessings {
		if b.Id == blessing {
			want.add(b.Mods, 1)
		}
	}
	assert.Equal(t, want, run.Mods())
	assert.Equal(t, content.Classes["warrior"].HP+want.Vigor, run.HP, "a run starts at full health, talents included")
	assert.Equal(t, run.MaxHP(), run.HP)

	again, err := NewRun(7, "warrior", map[string]int{"vitality": 2, "might": 9, "anchor": 1, "fortune": 1})
	require.NoError(t, err)
	assert.Equal(t, run.Blessings, again.Blessings, "the same seed rolls the same opening blessing")

	medic, err := NewRun(7, "mage", map[string]int{"medic": 1})
	require.NoError(t, err)
	medic.HP, medic.Choices = 10, []string{"rest"}
	require.NoError(t, medic.Enter(0, 0))
	assert.Equal(t, 10+medic.MaxHP()*(restHeal+10)/100, medic.HP)

	room, err := NewRun(7, "ranger", map[string]int{"vitality": 3, "might": 3, "fleet": 2, "spirit": 2})
	require.NoError(t, err)
	require.NoError(t, room.Enter(0, 0))
	assert.Equal(t, Mods{Damage: 12, Speed: 6, Regen: 30, Vigor: 24}, room.Room.Setup.Mods)
	_, err = New(room.Room.Seed, room.Room.Setup)
	require.NoError(t, err, "a talented run's room still opens in the fight rules")
}

// 退路：从没打完的房间里退出来要用一次，同一间房换个种子重新开始、敌人不变；退路用完再退出就是倒下。没有房间时退不了。
func TestRetreatRerollsTheRoomUntilTheyRunOut(t *testing.T) {
	run, err := NewRun(21, "mage", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, run.Retreat(5), ErrRunAction, "no room to leave")
	require.NoError(t, run.Enter(0, 100))
	first := *run.Room

	require.NoError(t, run.Retreat(160))
	assert.Equal(t, BaseRetreats-1, run.Retreats)
	assert.NotEqual(t, first.Seed, run.Room.Seed, "a retreat rerolls the fight's random numbers")
	assert.NotZero(t, run.Room.Seed)
	assert.Equal(t, int64(160), run.Room.Issued, "the clock for the replay check restarts")
	assert.Equal(t, first.Setup, run.Room.Setup, "same enemies, same health and mana as when the room was entered")
	rng := run.Rng
	require.NoError(t, run.Retreat(170))
	assert.Equal(t, rng, run.Rng, "retreating never touches the run's own dice")

	assert.Zero(t, run.Retreats)
	require.NoError(t, run.Retreat(180))
	assert.True(t, run.Over, "out of retreats, leaving the room is falling in it")
	assert.Zero(t, run.HP)
	assert.Nil(t, run.Room)
	assert.ErrorIs(t, run.Retreat(190), ErrRunAction)
}

// 护命符：挂上以后第一次倒下以半条命复活，同一间房换个种子重打，那次倒下没打出来的击杀不算；第二次倒下就结束。
func TestWardRevivesOnceInTheSameRoom(t *testing.T) {
	run, err := NewRun(33, "warrior", nil)
	require.NoError(t, err)
	require.NoError(t, run.UseItem(ItemWard))
	assert.ErrorIs(t, run.UseItem(ItemWard), ErrItemLimit, "one ward at a time")
	require.NoError(t, run.Enter(0, 0))
	seed, waves := run.Room.Seed, run.Room.Setup.Waves

	lost := &Fight{Over: true, Tick: 600, Kills: 4, Stones: 2, P: Player{HP: 0}}
	require.NoError(t, run.Clear(lost, 50))
	assert.False(t, run.Over)
	assert.False(t, run.Ward)
	assert.Equal(t, run.MaxHP()*wardHeal/100, run.HP)
	require.NotNil(t, run.Room)
	assert.Equal(t, run.HP, run.Room.Setup.HP, "the revived fight opens at the new health")
	assert.NotEqual(t, seed, run.Room.Seed)
	assert.Equal(t, waves, run.Room.Setup.Waves)
	assert.Zero(t, run.Kills+run.Stones, "the lost attempt's kills do not count")
	assert.Equal(t, 600, run.Ticks)

	require.NoError(t, run.Clear(lost, 60))
	assert.True(t, run.Over)
	assert.Equal(t, 4, run.Kills, "the fall that ends the run keeps its kills")
}

// 商店道具只能在两间房之间用：续命丹回四成生命，生命是满的不能用；福缘匣送一样还没有的祝福；每轮最多三件。
func TestItemsAreUsedBetweenRooms(t *testing.T) {
	run, err := NewRun(41, "ranger", nil)
	require.NoError(t, err)
	assert.ErrorIs(t, run.UseItem(ItemElixir), ErrItemLimit, "full health")
	assert.ErrorIs(t, run.UseItem("gold_pouch"), ErrRunAction, "items from the old card dungeon do nothing here")

	run.HP = 10
	require.NoError(t, run.UseItem(ItemElixir))
	assert.Equal(t, 10+run.MaxHP()*elixirHeal/100, run.HP)
	require.NoError(t, run.UseItem(ItemFortune))
	require.Len(t, run.Blessings, 1)
	assert.Equal(t, 2, run.Used)

	run.Choices = []string{"shrine"}
	require.NoError(t, run.Enter(0, 0))
	require.NotNil(t, run.Offer)
	assert.ErrorIs(t, run.UseItem(ItemWard), ErrRunAction, "pick first")
	require.NoError(t, run.Pick(-1, 0))

	require.NoError(t, run.UseItem(ItemWard))
	run.HP = 1
	assert.ErrorIs(t, run.UseItem(ItemElixir), ErrItemLimit, "three items per run")
	assert.Equal(t, 3, run.Used)

	run.Choices = []string{"fight"}
	require.NoError(t, run.Enter(0, 0))
	assert.ErrorIs(t, run.UseItem(ItemFortune), ErrRunAction, "not while a room is out")

	all, err := NewRun(41, "ranger", nil)
	require.NoError(t, err)
	for _, b := range content.Blessings {
		all.Blessings = append(all.Blessings, b.Id)
	}
	assert.ErrorIs(t, all.UseItem(ItemFortune), ErrItemLimit, "nothing left to give")
}

// 给页面看的一轮：已经发出的房间只剩种类，不再带种子和开局；刚进房或重新开始那一次带着，页面才能去打。
func TestPublicRunHidesAnIssuedRoom(t *testing.T) {
	run, err := NewRun(51, "warrior", nil)
	require.NoError(t, err)
	assert.Same(t, run, run.Public(false), "nothing to hide without a room")
	require.NoError(t, run.Enter(0, 0))

	assert.Same(t, run, run.Public(true))
	seen := run.Public(false)
	require.NotNil(t, seen.Room)
	assert.True(t, seen.Room.Hidden)
	assert.Equal(t, "fight", seen.Room.Kind)
	assert.Zero(t, seen.Room.Seed)
	assert.Empty(t, seen.Room.Setup.Waves)
	assert.NotZero(t, run.Room.Seed, "the real room stays on the server")
}

// 道韵：一轮结束按打通的房间与重数发，战绩记最深的一次；天赋要攒够道韵才能升，升到头不能再升。
func TestMetaSettlesRunsAndSpendsCrystals(t *testing.T) {
	meta := &Meta{}
	meta.Normalize()
	run, err := NewRun(61, "mage", nil)
	require.NoError(t, err)
	run.Floor, run.Step, run.Cleared = 3, 2, 9
	assert.Equal(t, 2, run.Floors())
	assert.Equal(t, 2*FloorSteps+2, run.Score(), "the score is steps: two floors and two steps")

	assert.Equal(t, 2*9+12*2, meta.Settle(run))
	assert.Equal(t, 42, meta.Crystals)
	assert.Equal(t, 42, meta.Earned)
	assert.Equal(t, 1, meta.Runs)
	assert.Equal(t, &Record{Runs: 1, Best: 2*FloorSteps + 2}, meta.Records["mage"])
	run.Floor, run.Step, run.Cleared = 1, 0, 2
	meta.Settle(run)
	assert.Equal(t, 2*FloorSteps+2, meta.Records["mage"].Best, "the record keeps the deepest run")
	assert.Equal(t, 2, meta.Records["mage"].Runs)

	meta.Crystals = 25
	assert.ErrorIs(t, meta.Buy("nonsense"), ErrUnknown)
	assert.ErrorIs(t, meta.Buy("fortune"), ErrCrystals)
	require.NoError(t, meta.Buy("vitality"))
	assert.Equal(t, 5, meta.Crystals)
	assert.Equal(t, 1, meta.Talents["vitality"])
	assert.ErrorIs(t, meta.Buy("vitality"), ErrCrystals, "the second level costs more")
	meta.Crystals = 1000
	require.NoError(t, meta.Buy("anchor"))
	assert.ErrorIs(t, meta.Buy("anchor"), ErrMaxed)
	assert.Equal(t, 1000-40, meta.Crystals)
}

// 背包：商店里没有的道具放不进去，每种最多存 MaxItemStack 件，拿空了就从背包里删掉。
func TestMetaBag(t *testing.T) {
	meta := &Meta{}
	meta.Normalize()
	assert.ErrorIs(t, meta.AddItem("gold_pouch", 1), ErrUnknown)
	assert.ErrorIs(t, meta.AddItem(ItemElixir, 0), ErrUnknown)
	require.NoError(t, meta.AddItem(ItemElixir, MaxItemStack))
	assert.ErrorIs(t, meta.AddItem(ItemElixir, 1), ErrItemLimit)
	assert.ErrorIs(t, meta.TakeItem(ItemWard), ErrNoItem)
	require.NoError(t, meta.AddItem(ItemWard, 1))
	require.NoError(t, meta.TakeItem(ItemWard))
	assert.NotContains(t, meta.Items, ItemWard)
}

// 天赋表自身：每种天赋至少一级、价格逐级不降，每一级给的东西至少有一样；所有天赋满级加上所有祝福仍在规则范围内。
func TestTalentTableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, talent := range content.Talents {
		assert.False(t, seen[talent.Id], talent.Id)
		seen[talent.Id] = true
		require.NotEmpty(t, talent.Costs, talent.Id)
		for i, cost := range talent.Costs {
			assert.Positive(t, cost, talent.Id)
			if i > 0 {
				assert.GreaterOrEqual(t, cost, talent.Costs[i-1], talent.Id)
			}
		}
		gives := talent.Mods != Mods{} || talent.Retreats > 0 || talent.Rest > 0 || talent.Blessings > 0
		assert.True(t, gives, "%s gives nothing", talent.Id)
	}
	assert.Equal(t, len(content.Talents), len(Talents()))
}
