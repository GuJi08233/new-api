package wendao

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v2.16.23 及以前写的存档没有格式版本：照常读出、缺的表补成空的，写回去时记上现在的版本，宗门与主角的进度都不变。
func TestOldSaveWithoutSchemaIsUpgraded(t *testing.T) {
	old := `{"sect":{"version":1,"buildings":{"hall":3},"res":{"stone":120,"herb":0,"ore":0,"rep":0},"disciples":[]},` +
		`"hero":{"class":"mage","realm":1,"layer":4,"cards":[{"id":"m_bolt"}],"deck":[0],"steps":7},` +
		`"abyss":{"crystals":30},"abyss_loot":{"day":"2026-10-31","runs":2}}`
	save, err := ParseSave(old)
	require.NoError(t, err)
	assert.Equal(t, SaveSchema, save.Schema)
	assert.Equal(t, 3, save.Sect.Buildings[BuildingHall])
	assert.Equal(t, 120, save.Sect.Res.Stone)
	assert.NotNil(t, save.Hero.Cave, "missing tables are filled in")
	assert.Equal(t, 7, save.Hero.Steps)
	assert.Equal(t, 2, save.AbyssLoot.Runs)

	data, err := save.Encode()
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, common.UnmarshalJsonStr(data, &raw))
	assert.Equal(t, float64(SaveSchema), raw["schema"])
	again, err := ParseSave(data)
	require.NoError(t, err)
	assert.Equal(t, save.Hero.Steps, again.Hero.Steps)
	assert.Equal(t, save.Sect.Res, again.Sect.Res)
}

// 更新版本的程序写的存档(退回旧版本时会遇到)读不了也改不了：UpdateSave 不调用 edit，也不返回要写回的内容。
func TestNewerSaveIsRefused(t *testing.T) {
	newer := fmt.Sprintf(`{"schema":%d,"sect":{"res":{"stone":5}},"future_field":{"x":1}}`, SaveSchema+1)
	_, err := ParseSave(newer)
	assert.ErrorIs(t, err, ErrNewerSave)

	called := false
	data, err := UpdateSave(newer, func(save *Save) error {
		called = true
		return nil
	})
	assert.ErrorIs(t, err, ErrNewerSave)
	assert.False(t, called)
	assert.Empty(t, data)

	current, err := ParseSave(fmt.Sprintf(`{"schema":%d}`, SaveSchema))
	require.NoError(t, err)
	assert.Equal(t, SaveSchema, current.Schema)
}
