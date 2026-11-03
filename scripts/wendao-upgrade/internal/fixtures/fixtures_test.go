package fixtures

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 容量样本必须能实际读写并满足活动互斥和席位限制，不能用非法叠加制造容量结论。
func TestCurrentFixturesKeepProgressAndRespectActivityLimits(t *testing.T) {
	samples, err := Build()
	require.NoError(t, err)
	again, err := Build()
	require.NoError(t, err)
	assert.Equal(t, samples, again, "the same rules produce byte-identical fixed samples")
	for _, sample := range samples {
		if sample.Kind != "save" || len(sample.Name) < 8 || sample.Name[:8] != "current_" {
			continue
		}
		t.Run(sample.Name, func(t *testing.T) {
			save, err := wendao.ParseSave(sample.Data)
			require.NoError(t, err)
			assert.Len(t, save.Hero.Cards, wendao.MaxCards)
			assert.Len(t, save.Hero.Nodes, len(wendao.JourneyNodes))
			assert.Len(t, save.Sect.Disciples, save.Sect.Cap())
			assert.Len(t, save.Receipts, wendao.ReceiptKeep)
			active := 0
			for _, present := range []bool{save.Hero.Battle != nil, save.Hero.Travel != nil, save.Hero.Meditation != nil, save.Duels.Active != nil} {
				if present {
					active++
				}
			}
			assert.LessOrEqual(t, active, 1)
			members := map[int]int{}
			for _, disciple := range save.Sect.Disciples {
				if disciple.Outpost > 0 {
					assert.Zero(t, disciple.Mission)
					assert.NotEmpty(t, disciple.OutpostToken)
					members[disciple.Outpost]++
				}
			}
			for _, count := range members {
				assert.LessOrEqual(t, count, wendao.OutpostSlots)
			}
			encoded, err := save.Encode()
			require.NoError(t, err)
			assert.Equal(t, sample.Data, encoded)
		})
	}
}

// 会话存的是控制器外层，不可把裸规则对象当作可恢复的秘境/大比状态来测容量。
func TestSessionSamplesIncludeRestorableStateEnvelopes(t *testing.T) {
	samples, err := Build()
	require.NoError(t, err)
	for _, sample := range samples {
		switch sample.Name {
		case "rift":
			var state struct {
				Run *doufa.Run `json:"run"`
			}
			require.NoError(t, common.UnmarshalJsonStr(sample.Data, &state))
			require.NotNil(t, state.Run)
			assert.Equal(t, doufa.RunStateVersion, state.Run.V)
			require.NotNil(t, state.Run.Room)
			_, err := doufa.New(state.Run.Room.Seed, state.Run.Room.Setup)
			require.NoError(t, err)
		case "tournament":
			var state struct {
				Steps      int                `json:"steps"`
				Tournament *wendao.Tournament `json:"tournament"`
			}
			require.NoError(t, common.UnmarshalJsonStr(sample.Data, &state))
			require.NotNil(t, state.Tournament)
			assert.Equal(t, wendao.StateVersion, state.Tournament.Version)
			assert.Len(t, state.Tournament.Fighters, wendao.TeamSize)
			assert.Equal(t, wendao.PhasePick, state.Tournament.Phase)
		}
	}
}

// 旧样本有可观察的资产契约：退还未用丹符一次，主线、问心阶和秘境成长不能被空值归一化掩盖丢失。
func TestLegacyFixtureRetainsAssetsAcrossUpgrade(t *testing.T) {
	samples, err := Build()
	require.NoError(t, err)
	for _, sample := range samples {
		if sample.Name != "legacy_cards" {
			continue
		}
		save, err := wendao.ParseSave(sample.Data)
		require.NoError(t, err)
		assert.Equal(t, 120, save.Sect.Res.Stone)
		assert.Equal(t, 7, save.Hero.Steps)
		assert.Equal(t, 1, save.Hero.Nodes["c1_oath"].Clears)
		assert.Nil(t, save.Hero.Battle)
		assert.Equal(t, map[string]int{dungeon.PotionFire: 1, dungeon.PotionHeal: 1}, save.Hero.Potions)
		assert.Equal(t, 225, save.Rift.Crystals)
		assert.Equal(t, 6, save.Rift.Items["elixir"])
		encoded, err := save.Encode()
		require.NoError(t, err)
		again, err := wendao.ParseSave(encoded)
		require.NoError(t, err)
		assert.Equal(t, save.Hero.Potions, again.Hero.Potions)
		assert.Equal(t, save.Rift, again.Rift)
		return
	}
	t.Fatal("legacy asset fixture is missing")
}
