package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupDescriptionsPersistIndependentlyFromUserUsableGroups(t *testing.T) {
	originalDescriptions := GroupDescriptions2JSONString()
	originalUsableGroups := UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDescriptionsByJSONString(originalDescriptions))
		require.NoError(t, UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, UpdateGroupDescriptionsByJSONString(`{"private":"长期保留"}`))
	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{}`))

	assert.JSONEq(t, `{"private":"长期保留"}`, GroupDescriptions2JSONString())
	assert.JSONEq(t, `{}`, UserUsableGroups2JSONString())
	assert.Empty(t, GetUserUsableGroupsCopy())
	assert.Equal(t, "长期保留", GetUsableGroupDescription("private"))

	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{"private":"旧描述"}`))
	assert.Equal(t, "长期保留", GetUserUsableGroupsCopy()["private"])
}

func TestInvalidGroupDescriptionsDoNotReplaceExistingValues(t *testing.T) {
	originalDescriptions := GroupDescriptions2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDescriptionsByJSONString(originalDescriptions))
	})

	require.NoError(t, UpdateGroupDescriptionsByJSONString(`{"default":"默认分组"}`))
	require.Error(t, CheckGroupDescriptions(`{"default":false}`))
	require.Error(t, UpdateGroupDescriptionsByJSONString(`{"default":false}`))
	assert.JSONEq(t, `{"default":"默认分组"}`, GroupDescriptions2JSONString())
}
