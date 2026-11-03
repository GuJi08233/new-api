package doufa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// won 是一间打完的房：剩多少生命与灵力(灵力存成百分之一点)、打了几帧、杀了几只、拾了几块灵石。
func won(hp, mp int) *Fight {
	return &Fight{Over: true, Won: true, P: Player{HP: hp, MP: mp * 100}, Tick: 900, Kills: 6, Stones: 3}
}

// 一轮从满状态开始，第一步只有一间战斗房；进去以后按这一重的敌人与房间开局，带上生命、灵力、法宝与祝福。
func TestRunStartsWithAFightRoom(t *testing.T) {
	run, err := NewRun(7, "ranger")
	require.NoError(t, err)
	spec := content.Classes["ranger"]
	assert.Equal(t, spec.HP, run.HP)
	assert.Equal(t, spec.Weapons, run.Weapons)
	assert.Equal(t, []string{"fight"}, run.Choices)
	_, err = NewRun(7, "monk")
	assert.ErrorIs(t, err, ErrSetup)

	require.NoError(t, run.Enter(0, 1000))
	require.NotNil(t, run.Room)
	assert.Equal(t, "fight", run.Room.Kind)
	assert.Equal(t, int64(1000), run.Room.Issued)
	setup := run.Room.Setup
	assert.Contains(t, floorPools[0].Rooms, setup.Room)
	assert.Len(t, setup.Waves, 2)
	for _, wave := range setup.Waves {
		n := 0
		for _, g := range wave {
			assert.Contains(t, floorPools[0].Foes, g.Foe)
			n += g.N
		}
		assert.GreaterOrEqual(t, n, 3)
		assert.LessOrEqual(t, n, 4)
	}
	assert.Equal(t, FoeScale{HP: 100, Dmg: 100}, setup.Scale)
	_, err = New(run.Room.Seed, setup)
	require.NoError(t, err, "the server's setup opens in the fight rules")
	assert.ErrorIs(t, run.Enter(0, 1000), ErrRunAction, "a room in progress blocks the next one")
}

// 打赢一间房记下剩下的生命与灵力、击杀和灵石，然后给出下一步；倒下就结束这一轮。没打完的房不能记。
func TestRunClearsRooms(t *testing.T) {
	run, err := NewRun(11, "warrior")
	require.NoError(t, err)
	assert.ErrorIs(t, run.Clear(won(10, 10)), ErrRunAction, "no room to clear")
	require.NoError(t, run.Enter(0, 0))
	assert.ErrorIs(t, run.Clear(&Fight{}), ErrRunAction, "the room is not over")
	require.NoError(t, run.Clear(won(77, 40)))
	assert.Equal(t, 77, run.HP)
	assert.Equal(t, 40, run.MP)
	assert.Equal(t, 1, run.Cleared)
	assert.Equal(t, 6, run.Kills)
	assert.Equal(t, 3, run.Stones)
	if run.Offer != nil {
		require.NoError(t, run.Pick(0, 0))
	}
	assert.Equal(t, 1, run.Step)
	assert.NotEmpty(t, run.Choices)

	for run.Room == nil {
		i := indexOf(run.Choices, "fight")
		if i < 0 {
			i = indexOf(run.Choices, "elite")
		}
		require.GreaterOrEqual(t, i, 0, "every step offers a fight: %v", run.Choices)
		require.NoError(t, run.Enter(i, 0))
	}
	require.NoError(t, run.Clear(&Fight{Over: true, Tick: 300, P: Player{HP: 0}}))
	assert.True(t, run.Over)
	assert.Zero(t, run.HP)
	assert.ErrorIs(t, run.Enter(0, 0), ErrRunAction)
	assert.ErrorIs(t, run.Abandon(), ErrRunAction)
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

// 每重最后一步只有首领；打倒首领回血、进下一重，并给三选一的祝福；下一重的敌人换成赤炎窟的，强度更高。
func TestRunBossLeadsToTheNextFloor(t *testing.T) {
	run, err := NewRun(5, "mage")
	require.NoError(t, err)
	run.Step = FloorSteps - 2
	run.Choices = nil
	run.next()
	assert.Equal(t, []string{"boss"}, run.Choices)
	require.NoError(t, run.Enter(0, 0))
	assert.Equal(t, [][]Group{{{Foe: "slime_king", N: 1}}}, run.Room.Setup.Waves)
	assert.Equal(t, "glade", run.Room.Setup.Room)

	require.NoError(t, run.Clear(won(20, 0)))
	assert.Equal(t, 2, run.Floor)
	assert.Equal(t, min(run.MaxHP(), 20+run.MaxHP()*bossHeal/100), run.HP)
	require.NotNil(t, run.Offer)
	assert.Equal(t, "blessing", run.Offer.Kind)
	assert.Len(t, run.Offer.Items, 3)
	assert.Equal(t, FloorSteps, run.Score(), "a beaten boss counts as a whole floor")
	blessing := run.Offer.Items[1]
	require.NoError(t, run.Pick(1, 0))
	assert.Equal(t, []string{blessing}, run.Blessings)
	assert.Equal(t, 0, run.Step)

	for run.Room == nil {
		i := indexOf(run.Choices, "fight")
		if i < 0 {
			i = indexOf(run.Choices, "elite")
		}
		require.NoError(t, run.Enter(i, 0))
	}
	setup := run.Room.Setup
	assert.Equal(t, FoeScale{HP: 130, Dmg: 112}, setup.Scale)
	for _, wave := range setup.Waves {
		for _, g := range wave {
			assert.Contains(t, append(floorPools[1].Foes, floorPools[1].Elite), g.Foe)
		}
	}
	assert.Equal(t, run.Mods(), setup.Mods)
}

// 祝福叠成加成并进到开局里，生命上限跟着变；宝库的法宝换掉指定的那一件；静修回血；不要也可以。
func TestRunOffersAndRest(t *testing.T) {
	run, err := NewRun(3, "warrior")
	require.NoError(t, err)
	run.Blessings = []string{"edge", "vigor", "ward"}
	assert.Equal(t, Mods{Damage: 20, Vigor: 25, Ward: 1}, run.Mods())
	assert.Equal(t, content.Classes["warrior"].HP+25, run.MaxHP())

	run.Choices = []string{"treasure"}
	require.NoError(t, run.Enter(0, 0))
	require.NotNil(t, run.Offer)
	assert.Equal(t, "weapon", run.Offer.Kind)
	require.Len(t, run.Offer.Items, 2)
	for _, w := range run.Offer.Items {
		assert.NotContains(t, run.Weapons, w, "the treasury offers weapons not already carried")
	}
	assert.ErrorIs(t, run.Pick(0, 2), ErrRunAction, "only two slots")
	picked := run.Offer.Items[0]
	kept := run.Weapons[0]
	require.NoError(t, run.Pick(0, 1))
	assert.Equal(t, []string{kept, picked}, run.Weapons)

	run.HP = 10
	run.Choices = []string{"rest"}
	require.NoError(t, run.Enter(0, 0))
	assert.Equal(t, 10+run.MaxHP()*restHeal/100, run.HP)

	run.Choices = []string{"shrine"}
	require.NoError(t, run.Enter(0, 0))
	require.NotNil(t, run.Offer)
	for _, b := range run.Offer.Items {
		assert.NotContains(t, run.Blessings, b, "a blessing is offered once")
	}
	require.NoError(t, run.Pick(-1, 0))
	assert.Nil(t, run.Offer)
	assert.Len(t, run.Blessings, 3)
}

// 同一个种子走同样的一轮：房间、敌人、选项都一样，服务端重启以后读回来的状态接着走也一样。
func TestRunIsDeterministic(t *testing.T) {
	play := func() []string {
		run, err := NewRun(99, "ranger")
		require.NoError(t, err)
		var trace []string
		for step := 0; step < 12 && !run.Over; step++ {
			if run.Offer != nil {
				require.NoError(t, run.Pick(0, 1))
				continue
			}
			require.NoError(t, run.Enter(0, 0))
			if run.Room != nil {
				trace = append(trace, run.Room.Setup.Room)
				require.NoError(t, run.Clear(won(50, 50)))
			}
			trace = append(trace, run.Choices...)
		}
		return trace
	}
	assert.Equal(t, play(), play())
}

// 祝福、宝库与每一重的敌人和房间都要认识，祝福的加成要在规则允许的范围里，否则发出去的房间开不了局。
func TestRunContentIsKnown(t *testing.T) {
	seen := map[string]bool{}
	all := Mods{}
	for _, b := range content.Blessings {
		assert.False(t, seen[b.Id], b.Id)
		seen[b.Id] = true
		assert.True(t, b.Mods.valid(), b.Id)
		all.Damage += b.Mods.Damage
		all.Speed += b.Mods.Speed
		all.Haste += b.Mods.Haste
		all.Pierce += b.Mods.Pierce
		all.Regen += b.Mods.Regen
		all.Vigor += b.Mods.Vigor
		all.Dodge += b.Mods.Dodge
		all.Focus += b.Mods.Focus
		all.Reach += b.Mods.Reach
		all.Ward += b.Mods.Ward
	}
	assert.True(t, all.valid(), "every blessing together stays within the rules")
	for _, w := range content.WeaponPool {
		assert.Contains(t, content.Weapons, w)
	}
	for _, pool := range floorPools {
		for _, room := range append(pool.Rooms, pool.BossRoom) {
			assert.Contains(t, content.Rooms, room)
		}
		for _, foe := range append(pool.Foes, pool.Elite, pool.Boss) {
			assert.Contains(t, content.Foes, foe)
		}
		assert.Equal(t, "boss", content.Foes[pool.Boss].AI)
	}
}
