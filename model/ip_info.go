package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ipInfoCacheStateName      = "current"
	legacyIpInfoCacheStateKey = "__ip_info_cache_state__"
)

// IpInfo 缓存 IP 归属地查询结果。一个 IP 只查询一次外部接口，之后直接读库。
// District/Latitude/Longitude/Postal/Asn/Org 为可选字段，不同 provider 命中程度不同，
// 空值落库即可，AutoMigrate 均允许 NULL，跨 SQLite/MySQL/PG 安全。
type IpInfo struct {
	Id         int    `json:"-" gorm:"primaryKey"`
	Ip         string `json:"ip" gorm:"size:64;uniqueIndex:idx_ip_info_ip"`
	Continent  string `json:"continent" gorm:"size:32"`
	Country    string `json:"country" gorm:"size:64"`
	Province   string `json:"province" gorm:"size:64"`
	City       string `json:"city" gorm:"size:64"`
	District   string `json:"district" gorm:"size:64"`
	Latitude   string `json:"latitude" gorm:"size:32"`
	Longitude  string `json:"longitude" gorm:"size:32"`
	Postal     string `json:"postal" gorm:"size:32"`
	Asn        string `json:"asn" gorm:"size:32"`
	Org        string `json:"org" gorm:"size:128"`
	Isp        string `json:"isp" gorm:"size:128"`
	Provider   string `json:"provider" gorm:"size:16"`
	Generation int64  `json:"-" gorm:"not null;default:0"`
	UpdatedAt  int64  `json:"updated_at"`
}

func (IpInfo) TableName() string {
	return "ip_infos"
}

// IpInfoCacheState stores cache metadata separately from cached IP records.
// The table currently has one row named "current".
type IpInfoCacheState struct {
	Name       string `gorm:"primaryKey;size:32"`
	Generation int64  `gorm:"not null"`
}

func (IpInfoCacheState) TableName() string {
	return "ip_info_cache_states"
}

// initializeIpInfoCacheState runs after AutoMigrate. It also moves the legacy
// sentinel row out of ip_infos so request-time reads never need to create or
// exclude metadata records.
func initializeIpInfoCacheState() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		generation := int64(0)
		var legacyState IpInfo
		legacyResult := tx.Where("ip = ?", legacyIpInfoCacheStateKey).
			Limit(1).
			Find(&legacyState)
		if legacyResult.Error != nil {
			return legacyResult.Error
		}
		if legacyResult.RowsAffected == 1 {
			generation = legacyState.Generation
		}

		state := IpInfoCacheState{
			Name:       ipInfoCacheStateName,
			Generation: generation,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoNothing: true,
		}).Create(&state).Error; err != nil {
			return err
		}

		return tx.Where("ip = ?", legacyIpInfoCacheStateKey).Delete(&IpInfo{}).Error
	})
}

func GetIpInfo(ip string) (*IpInfo, error) {
	var info IpInfo
	currentGeneration := DB.Model(&IpInfoCacheState{}).
		Select("generation").
		Where("name = ?", ipInfoCacheStateName)
	err := DB.Where("ip = ? AND generation = (?)", ip, currentGeneration).First(&info).Error
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// GetIpInfoCacheGeneration returns the current cache generation without any
// write-side effect. The state row is created during database migration.
func GetIpInfoCacheGeneration() (int64, error) {
	var state IpInfoCacheState
	err := DB.Where("name = ?", ipInfoCacheStateName).Take(&state).Error
	if err != nil {
		return 0, err
	}
	return state.Generation, nil
}

// SaveIpInfo writes an IP lookup tagged with the generation captured when the
// lookup started. A nil generation retries the state read at write time, which
// keeps caching fail-open after a transient earlier read failure.
//
// The UPSERT is monotonic: an older in-flight lookup cannot overwrite a newer
// generation. An old-generation insert that races with a reset remains
// invisible to GetIpInfo and is removed by a later reset.
func SaveIpInfo(info *IpInfo, generation *int64) error {
	if info == nil || info.Ip == "" {
		return nil
	}

	if generation == nil {
		currentGeneration, err := GetIpInfoCacheGeneration()
		if err != nil {
			return err
		}
		generation = &currentGeneration
	}

	cacheGeneration := *generation
	info.Generation = cacheGeneration
	info.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ip"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"continent":  gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE continent END", cacheGeneration, info.Continent),
			"country":    gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE country END", cacheGeneration, info.Country),
			"province":   gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE province END", cacheGeneration, info.Province),
			"city":       gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE city END", cacheGeneration, info.City),
			"district":   gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE district END", cacheGeneration, info.District),
			"latitude":   gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE latitude END", cacheGeneration, info.Latitude),
			"longitude":  gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE longitude END", cacheGeneration, info.Longitude),
			"postal":     gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE postal END", cacheGeneration, info.Postal),
			"asn":        gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE asn END", cacheGeneration, info.Asn),
			"org":        gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE org END", cacheGeneration, info.Org),
			"isp":        gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE isp END", cacheGeneration, info.Isp),
			"provider":   gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE provider END", cacheGeneration, info.Provider),
			"generation": gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE generation END", cacheGeneration, cacheGeneration),
			"updated_at": gorm.Expr("CASE WHEN generation <= ? THEN ? ELSE updated_at END", cacheGeneration, info.UpdatedAt),
		}),
	}).Create(info).Error
}

// ClearAllIpInfo advances the cache generation and removes every older cache
// row in one transaction. In-flight writes from an older generation may finish
// later, but they are never visible and are cleaned by the next reset.
func ClearAllIpInfo() (int64, error) {
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&IpInfoCacheState{}).
			Where("name = ?", ipInfoCacheStateName).
			UpdateColumn("generation", gorm.Expr("generation + ?", 1))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("ip info cache state is not initialized")
		}

		var state IpInfoCacheState
		if err := tx.Where("name = ?", ipInfoCacheStateName).Take(&state).Error; err != nil {
			return err
		}

		result = tx.Where("generation < ?", state.Generation).Delete(&IpInfo{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}
