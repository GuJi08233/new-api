package authz

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyScopedPoliciesRemainRestrictedAfterReseeding(t *testing.T) {
	db := newAuthzTestDB(t)
	for _, subject := range []string{RoleSubject(BuiltInRoleAdmin), UserSubject(9)} {
		rule := model.CasbinRule{Ptype: "p", V0: subject, V1: ChannelRead.Resource, V2: ChannelRead.Action, V3: EffectAllow, V4: "own"}
		require.NoError(t, db.Create(&rule).Error)
	}
	require.NoError(t, Init(db))
	assert.False(t, Can(2, common.RoleAdminUser, ChannelRead))
	assert.False(t, Can(9, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelOperate))
	require.NoError(t, Init(db))
	assert.False(t, Can(2, common.RoleAdminUser, ChannelRead))
	assert.False(t, Can(9, common.RoleAdminUser, ChannelRead))
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v4 = ?", "own").Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
