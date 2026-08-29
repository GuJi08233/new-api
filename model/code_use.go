package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// 一码多用(max_uses > 1)的码需要"每用户限用一次"约束,
// 否则单个用户可以独占全部使用次数刷额度。本表记录多用码的核销明细,
// (code_type, code_id, user_id) 唯一索引在并发下兜底防重。
// 单次码由状态 CAS 天然防重,不写本表。

const (
	CodeUseTypeRedemption = "redemption"
	CodeUseTypeInvitation = "invitation"
)

type CodeUse struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	CodeType  string `json:"code_type" gorm:"type:varchar(16);uniqueIndex:idx_code_uses_unique,priority:1"`
	CodeId    int    `json:"code_id" gorm:"uniqueIndex:idx_code_uses_unique,priority:2"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex:idx_code_uses_unique,priority:3;index:idx_code_uses_user"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

// hasCodeUseInTx 查询用户是否已核销过该码,在持有码行锁的事务内调用。
func hasCodeUseInTx(tx *gorm.DB, codeType string, codeId int, userId int) (bool, error) {
	var count int64
	err := tx.Model(&CodeUse{}).
		Where("code_type = ? AND code_id = ? AND user_id = ?", codeType, codeId, userId).
		Count(&count).Error
	return count > 0, err
}

// insertCodeUseInTx 写入一条核销记录,唯一索引冲突表示并发下同一用户重复核销。
func insertCodeUseInTx(tx *gorm.DB, codeType string, codeId int, userId int) error {
	return tx.Create(&CodeUse{
		CodeType:  codeType,
		CodeId:    codeId,
		UserId:    userId,
		CreatedAt: common.GetTimestamp(),
	}).Error
}
