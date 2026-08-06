package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIpInfoCacheGenerationRejectsStaleSave(t *testing.T) {
	truncateTables(t)

	generation, err := GetIpInfoCacheGeneration()
	require.NoError(t, err)

	first := &IpInfo{Ip: "8.8.8.8", Country: "old", Provider: "gitee"}
	require.NoError(t, SaveIpInfo(first, &generation))

	deleted, err := ClearAllIpInfo()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	_, err = GetIpInfo(first.Ip)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	stale := &IpInfo{Ip: first.Ip, Country: "stale", Provider: "gitee"}
	require.NoError(t, SaveIpInfo(stale, &generation))
	_, err = GetIpInfo(stale.Ip)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	nextGeneration, err := GetIpInfoCacheGeneration()
	require.NoError(t, err)
	assert.Equal(t, generation+1, nextGeneration)

	fresh := &IpInfo{Ip: first.Ip, Country: "fresh", Provider: "ipwhois"}
	require.NoError(t, SaveIpInfo(fresh, &nextGeneration))

	cached, err := GetIpInfo(fresh.Ip)
	require.NoError(t, err)
	assert.Equal(t, "fresh", cached.Country)
	assert.Equal(t, nextGeneration, cached.Generation)

	lateStale := &IpInfo{Ip: first.Ip, Country: "late stale", Provider: "gitee"}
	require.NoError(t, SaveIpInfo(lateStale, &generation))
	cached, err = GetIpInfo(fresh.Ip)
	require.NoError(t, err)
	assert.Equal(t, "fresh", cached.Country)
	assert.Equal(t, nextGeneration, cached.Generation)
}

func TestInitializeIpInfoCacheStateMigratesLegacySentinel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Where("name = ?", ipInfoCacheStateName).Delete(&IpInfoCacheState{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("name = ?", ipInfoCacheStateName).Delete(&IpInfoCacheState{}).Error)
		require.NoError(t, initializeIpInfoCacheState())
	})

	_, err := GetIpInfoCacheGeneration()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	legacy := &IpInfo{
		Ip:         legacyIpInfoCacheStateKey,
		Generation: 42,
	}
	require.NoError(t, DB.Create(legacy).Error)
	require.NoError(t, initializeIpInfoCacheState())

	generation, err := GetIpInfoCacheGeneration()
	require.NoError(t, err)
	assert.Equal(t, int64(42), generation)

	var legacyCount int64
	require.NoError(t, DB.Model(&IpInfo{}).
		Where("ip = ?", legacyIpInfoCacheStateKey).
		Count(&legacyCount).Error)
	assert.Zero(t, legacyCount)
}

func TestSaveIpInfoWithoutGenerationUsesCurrentGeneration(t *testing.T) {
	truncateTables(t)

	wantGeneration, err := GetIpInfoCacheGeneration()
	require.NoError(t, err)
	info := &IpInfo{Ip: "1.1.1.1", Country: "fallback", Provider: "ipwhois"}
	require.NoError(t, SaveIpInfo(info, nil))

	cached, err := GetIpInfo(info.Ip)
	require.NoError(t, err)
	assert.Equal(t, "fallback", cached.Country)
	assert.Equal(t, wantGeneration, cached.Generation)
}
