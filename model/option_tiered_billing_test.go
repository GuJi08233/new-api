package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateOptionRejectsInvalidTieredExpressionBeforePersistence(t *testing.T) {
	originalDB := DB
	t.Cleanup(func() {
		DB = originalDB
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, DB.AutoMigrate(&Option{}))

	err = UpdateOption(billing_setting.BillingExprOptionKey, `{"model-a":"p + ("}`)
	require.Error(t, err)

	var stored Option
	queryErr := DB.Where("key = ?", billing_setting.BillingExprOptionKey).First(&stored).Error
	assert.ErrorIs(t, queryErr, gorm.ErrRecordNotFound)
}
