package model

import (
	"time"

	"gorm.io/gorm/clause"
)

// IpInfo 缓存 IP 归属地查询结果。一个 IP 只查询一次外部接口，之后直接读库。
type IpInfo struct {
	Id        int    `json:"-" gorm:"primaryKey"`
	Ip        string `json:"ip" gorm:"size:64;uniqueIndex:idx_ip_info_ip"`
	Continent string `json:"continent" gorm:"size:32"`
	Country   string `json:"country" gorm:"size:64"`
	Province  string `json:"province" gorm:"size:64"`
	City      string `json:"city" gorm:"size:64"`
	Isp       string `json:"isp" gorm:"size:128"`
	Provider  string `json:"provider" gorm:"size:16"`
	UpdatedAt int64  `json:"updated_at"`
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

// SaveIpInfo 写入或覆盖某 IP 的归属地缓存。并发查询同一个新 IP 时后写覆盖先写，
// 两次结果来自同一批提供方，覆盖无害。
func SaveIpInfo(info *IpInfo) error {
	if info == nil || info.Ip == "" {
		return nil
	}
	info.UpdatedAt = time.Now().Unix()
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"continent", "country", "province", "city", "isp", "provider", "updated_at",
		}),
	}).Create(info).Error
}
