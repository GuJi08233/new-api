package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMissingModelsHonorsConfiguredNameRules(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Ability{}, &Model{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM models").Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, DB.Exec("DELETE FROM models").Error)
	})

	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "gpt-4o", ChannelId: 901, Enabled: true},
		{Group: "vip", Model: "gpt-4.1", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "claude-3-opus", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "text-embedding-3-small", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "exact-model", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "exact-model-variant", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "unconfigured-model", ChannelId: 901, Enabled: true},
		{Group: "default", Model: "disabled-only-model", ChannelId: 901, Enabled: false},
	}).Error)
	require.NoError(t, DB.Create(&[]Model{
		{ModelName: "gpt-", NameRule: NameRulePrefix},
		{ModelName: "-opus", NameRule: NameRuleSuffix},
		{ModelName: "embedding", NameRule: NameRuleContains},
		{ModelName: "exact-model", NameRule: NameRuleExact},
	}).Error)

	globalMissing, err := GetMissingModels("")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"exact-model-variant", "unconfigured-model"}, globalMissing)

	defaultMissing, err := GetMissingModels("default")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"exact-model-variant", "unconfigured-model"}, defaultMissing)

	vipMissing, err := GetMissingModels("vip")
	require.NoError(t, err)
	assert.Empty(t, vipMissing)
}
