package doufa

import (
	"bytes"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const browserContent = "../../web/classic/src/pages/Games/wendao/doufa/content.json"

// 浏览器那份规则跑出来的操作记录，在服务端重放要得到一模一样的结果(含逐帧校验和)：这是服务端能核对斗法胜负的前提。
// 规则改了就用 bun pkg/doufa/testdata/generate_golden.mjs 重新生成，前端的 engine.test.js 也按同一份记录核对。
func TestReplayMatchesBrowser(t *testing.T) {
	data, err := os.ReadFile("testdata/golden.json")
	require.NoError(t, err)
	var records []struct {
		Name    string  `json:"name"`
		Seed    uint32  `json:"seed"`
		Setup   Setup   `json:"setup"`
		Inputs  string  `json:"inputs"`
		Summary Summary `json:"summary"`
	}
	require.NoError(t, common.Unmarshal(data, &records))
	require.NotEmpty(t, records)
	won, lost := 0, 0
	for _, record := range records {
		t.Run(record.Name, func(t *testing.T) {
			inputs, err := DecodeInputs(record.Inputs)
			require.NoError(t, err)
			fight, err := Replay(record.Seed, record.Setup, inputs)
			require.NoError(t, err)
			assert.Equal(t, record.Summary, fight.Summary())
		})
		if record.Summary.Won {
			won++
		} else if record.Summary.HP == 0 {
			lost++
		}
	}
	assert.Positive(t, won, "the records include a cleared room")
	assert.Positive(t, lost, "the records include a defeat")
}

// 数值与房间只有 pkg/doufa/content.json 一份，前端读的复制件必须一字不差；UPDATE_CONTENT=1 时重新复制。
func TestBrowserContentIsCurrent(t *testing.T) {
	if os.Getenv("UPDATE_CONTENT") == "1" {
		require.NoError(t, os.WriteFile(browserContent, contentJSON, 0o644))
	}
	copied, err := os.ReadFile(browserContent)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(contentJSON, copied), "run UPDATE_CONTENT=1 go test ./pkg/doufa to copy content.json to the browser")
}

// 每个流派的法宝与法术、试剑台的敌人与房间都要认识，房间要有主角与首领的站位，否则开不了局。
func TestContentIsWellFormed(t *testing.T) {
	for name, class := range content.Classes {
		require.Len(t, class.Weapons, 2, name)
		for _, w := range class.Weapons {
			assert.Contains(t, content.Weapons, w, name)
		}
		assert.Contains(t, []string{"quake", "nova", "blades"}, class.Skill.Kind, name)
		fight, err := New(1, TrialSetup(name))
		require.NoError(t, err, name)
		assert.Equal(t, class.HP, fight.P.HP)
		assert.Equal(t, class.MP*100, fight.P.MP)
	}
	for name, foe := range content.Foes {
		assert.Contains(t, []string{"chase", "shoot", "charge", "turret", "boss"}, foe.AI, name)
		if foe.AI == "boss" {
			require.NotNil(t, foe.Boss, name)
			assert.Contains(t, content.Foes, foe.Boss.Summon.Kind, name)
		}
		if foe.AI == "shoot" || foe.AI == "turret" {
			assert.Positive(t, foe.Shot.Count, name)
		}
	}
	for name, rows := range content.Rooms {
		players, bosses := 0, 0
		for _, row := range rows {
			assert.Len(t, row, len(rows[0]), name)
			players += bytes.Count([]byte(row), []byte("P"))
			bosses += bytes.Count([]byte(row), []byte("B"))
		}
		assert.Equal(t, 1, players, name)
		assert.Equal(t, 1, bosses, name)
	}
}

// 开局不认识的流派、法宝、房间或敌人都拒绝。
func TestNewRejectsUnknownSetup(t *testing.T) {
	good := TrialSetup("warrior")
	for _, edit := range []func(*Setup){
		func(s *Setup) { s.Class = "monk" },
		func(s *Setup) { s.Weapons = []string{"spear"} },
		func(s *Setup) { s.Weapons = nil },
		func(s *Setup) { s.Weapons = []string{"sword", "sword", "sword"} },
		func(s *Setup) { s.Room = "cave" },
		func(s *Setup) { s.Waves = nil },
		func(s *Setup) { s.Waves = [][]Group{{{Foe: "dragon", N: 1}}} },
		func(s *Setup) { s.Waves = [][]Group{{{Foe: "slime", N: 13}}} },
	} {
		setup := good
		edit(&setup)
		_, err := New(1, setup)
		assert.ErrorIs(t, err, ErrSetup)
	}
}

// 操作记录不合法时拒绝：超过帧数上限、输入越界、打完以后还有操作。
func TestReplayRejectsBadInputs(t *testing.T) {
	setup := TrialSetup("warrior")
	_, err := Replay(1, setup, make([]int, MaxTicks+1))
	assert.ErrorIs(t, err, ErrReplay)
	for _, bad := range []int{-1, 1 << 18, Pack(Dirs, -1, 0), Pack(-1, Dirs, 0)} {
		_, err := Replay(1, setup, []int{bad})
		assert.ErrorIs(t, err, ErrReplay, bad)
	}
	// 站着不动、一直不还手，迟早会倒下；倒下以后再多一帧就不合法。
	var inputs []int
	fight, err := New(1, setup)
	require.NoError(t, err)
	for !fight.Over {
		inputs = append(inputs, Pack(-1, -1, 0))
		fight.Step(Pack(-1, -1, 0))
	}
	require.Zero(t, fight.P.HP)
	_, err = Replay(1, setup, inputs)
	require.NoError(t, err)
	_, err = Replay(1, setup, append(inputs, Pack(-1, -1, 0)))
	assert.ErrorIs(t, err, ErrReplay)
}

// 传输格式压缩相邻相同的输入，往返不丢；坏的字符串、帧数为 0 的段、越界的输入与超长的记录都拒绝。
func TestInputsRoundTrip(t *testing.T) {
	inputs := []int{0, 0, 0, Pack(3, -1, BtnAttack), Pack(3, -1, BtnAttack), Pack(63, 12, BtnAttack|BtnDodge), 0}
	encoded := EncodeInputs(inputs)
	decoded, err := DecodeInputs(encoded)
	require.NoError(t, err)
	assert.Equal(t, inputs, decoded)
	assert.Equal(t, "", EncodeInputs(nil))

	for _, bad := range []string{"%%%", EncodeInputs([]int{1 << 18}), "AAA", EncodeInputs(make([]int, MaxTicks+1))} {
		_, err := DecodeInputs(bad)
		assert.ErrorIs(t, err, ErrReplay, bad)
	}
}

// 冲刺中与挨打后的无敌帧里不再受伤；近战扇形打得到身前的敌人，打不到身后的。
func TestDodgeAndMeleeArc(t *testing.T) {
	fight, err := New(1, TrialSetup("warrior"))
	require.NoError(t, err)
	fight.P.Dash = 3
	assert.False(t, fight.hurt(10))
	fight.P.Dash = 0
	assert.True(t, fight.hurt(10))
	assert.False(t, fight.hurt(10), "invulnerable right after a hit")
	assert.Equal(t, content.Classes["warrior"].HP-10, fight.P.HP)

	fight, err = New(1, TrialSetup("warrior"))
	require.NoError(t, err)
	fight.MarkT = 0
	fight.Marks = nil
	ahead := fight.spawn("slime", fight.P.X+px(40), fight.P.Y)
	behind := fight.spawn("slime", fight.P.X-px(40), fight.P.Y)
	fight.P.Face = 0
	fight.fire()
	assert.Less(t, ahead.HP, ahead.MaxHP)
	assert.Equal(t, behind.MaxHP, behind.HP)
}
