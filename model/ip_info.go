package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ipInfoCacheStateKey = "__ip_info_cache_state__"

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

func GetIpInfo(ip string) (*IpInfo, error) {
	var info IpInfo
	err := DB.Where("ip = ?", ip).First(&info).Error
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func getIpInfoCacheState(tx *gorm.DB, forUpdate bool) (*IpInfo, error) {
	var state IpInfo
	query := tx
	if forUpdate {
		query = lockForUpdate(query)
	}
	err := query.Where("ip = ?", ipInfoCacheStateKey).First(&state).Error
	if err == nil {
		return &state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	state = IpInfo{Ip: ipInfoCacheStateKey}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoNothing: true,
	}).Create(&state).Error; err != nil {
		return nil, err
	}

	query = tx
	if forUpdate {
		query = lockForUpdate(query)
	}
	if err := query.Where("ip = ?", ipInfoCacheStateKey).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

// GetIpInfoCacheGeneration 返回当前缓存代次。查询开始时记录代次，写入时再次校验，
// 可防止清空操作完成后仍被旧的在途查询重新写入。
func GetIpInfoCacheGeneration() (int64, error) {
	state, err := getIpInfoCacheState(DB, false)
	if err != nil {
		return 0, err
	}
	return state.Generation, nil
}

// SaveIpInfo 写入或覆盖某 IP 的归属地缓存。仅当查询开始时记录的 generation
// 仍为当前代次时写入；返回 false 表示清空操作已使本次查询结果过期。
func SaveIpInfo(info *IpInfo, generation int64) (bool, error) {
	if info == nil || info.Ip == "" {
		return false, nil
	}

	saved := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := getIpInfoCacheState(tx, true)
		if err != nil {
			return err
		}
		if state.Generation != generation {
			return nil
		}

		info.Generation = generation
		info.UpdatedAt = time.Now().Unix()
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ip"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"continent", "country", "province", "city", "district",
				"latitude", "longitude", "postal", "asn", "org",
				"isp", "provider", "generation", "updated_at",
			}),
		}).Create(info).Error; err != nil {
			return err
		}
		saved = true
		return nil
	})
	return saved, err
}

// ClearAllIpInfo 推进缓存代次并清空所有归属地记录，返回删除行数。
// 代次更新与删除在同一事务内完成，旧的在途查询无法在清空后重新写入。
func ClearAllIpInfo() (int64, error) {
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := getIpInfoCacheState(tx, true)
		if err != nil {
			return err
		}
		if err := tx.Model(&IpInfo{}).
			Where("ip = ?", ipInfoCacheStateKey).
			Update("generation", state.Generation+1).Error; err != nil {
			return err
		}

		result := tx.Where("ip <> ?", ipInfoCacheStateKey).Delete(&IpInfo{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}
