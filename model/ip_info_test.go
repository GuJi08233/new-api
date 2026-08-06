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
	saved, err := SaveIpInfo(first, generation)
	require.NoError(t, err)
	require.True(t, saved)

	deleted, err := ClearAllIpInfo()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	_, err = GetIpInfo(first.Ip)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	stale := &IpInfo{Ip: first.Ip, Country: "stale", Provider: "gitee"}
	saved, err = SaveIpInfo(stale, generation)
	require.NoError(t, err)
	assert.False(t, saved)
	_, err = GetIpInfo(stale.Ip)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	nextGeneration, err := GetIpInfoCacheGeneration()
	require.NoError(t, err)
	assert.Equal(t, generation+1, nextGeneration)

	fresh := &IpInfo{Ip: first.Ip, Country: "fresh", Provider: "ipwhois"}
	saved, err = SaveIpInfo(fresh, nextGeneration)
	require.NoError(t, err)
	require.True(t, saved)

	cached, err := GetIpInfo(fresh.Ip)
	require.NoError(t, err)
	assert.Equal(t, "fresh", cached.Country)
	assert.Equal(t, nextGeneration, cached.Generation)
}
