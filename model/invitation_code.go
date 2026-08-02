package model

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

type InvitationCode struct {
	Id          int            `json:"id"`
	UserId      int            `json:"user_id" gorm:"index"`                               // 生成者 ID
	Code        string         `json:"code" gorm:"type:varchar(32);uniqueIndex"`           // 邀请码
	Quota       int            `json:"quota" gorm:"default:0"`                             // 生成时消耗的额度
	Status      int            `json:"status" gorm:"default:1"`                            // 1=未使用, 2=已使用, 3=已禁用
	UsedUserId  int            `json:"used_user_id"`                                       // 使用者 ID
	UsedTime    int64          `json:"used_time" gorm:"bigint"`                            // 使用时间
	CreatedTime int64          `json:"created_time" gorm:"bigint"`                         // 创建时间
	Count       int            `json:"count" gorm:"-:all"`                                 // 仅用于批量创建请求
	Remark      string         `json:"remark" gorm:"type:varchar(255)" validate:"max=255"` // 备注
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func GetAllInvitationCodes(startIdx int, num int) (codes []*InvitationCode, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&InvitationCode{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&codes).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return codes, total, nil
}

func SearchInvitationCodes(keyword string, startIdx int, num int) (codes []*InvitationCode, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&InvitationCode{})

	if id, err := strconv.Atoi(keyword); err == nil {
		query = query.Where("id = ? OR code LIKE ? OR remark LIKE ?", id, keyword+"%", keyword+"%")
	} else {
		query = query.Where("code LIKE ? OR remark LIKE ?", keyword+"%", keyword+"%")
	}

	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&codes).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return codes, total, nil
}

func GetMyInvitationCodes(userId int, startIdx int, num int) (codes []*InvitationCode, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&InvitationCode{}).Where("user_id = ?", userId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&codes).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return codes, total, nil
}

func GetInvitationCodeById(id int) (*InvitationCode, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	code := InvitationCode{Id: id}
	err := ReadDB().First(&code, "id = ?", id).Error
	return &code, err
}

func GetInvitationCodeByCode(codeStr string) (*InvitationCode, error) {
	if codeStr == "" {
		return nil, errors.New("邀请码为空")
	}
	var code InvitationCode
	err := ReadDB().Where("code = ?", codeStr).First(&code).Error
	if err != nil {
		return nil, err
	}
	return &code, nil
}

var (
	ErrInvitationCodeNotFound  = errors.New("无效的邀请码")
	ErrInvitationCodeNotUsable = errors.New("该邀请码已被使用或已禁用")
)

// ReserveInvitationCode 在创建用户之前原子占用邀请码（状态置为已使用），
// 防止并发注册用同一个码同时通过校验。占用成功后必须调用
// FinalizeInvitationCodeUsage 归属到新用户；建号失败时调用 ReleaseInvitationCode 释放。
func ReserveInvitationCode(codeStr string) (*InvitationCode, error) {
	if codeStr == "" {
		return nil, ErrInvitationCodeNotFound
	}
	common.RandomSleep()
	var code InvitationCode
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("code = ?", codeStr).First(&code).Error; err != nil {
			return ErrInvitationCodeNotFound
		}
		if code.Status != common.InvitationCodeStatusEnabled {
			return ErrInvitationCodeNotUsable
		}
		code.Status = common.InvitationCodeStatusUsed
		code.UsedTime = common.GetTimestamp()
		return tx.Save(&code).Error
	})
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// FinalizeInvitationCodeUsage 将已占用的邀请码归属到注册成功的用户，并发放奖励额度
func FinalizeInvitationCodeUsage(code *InvitationCode, userId int) error {
	if code == nil {
		return nil
	}
	if userId == 0 {
		return errors.New("无效的 user id")
	}
	rewardQuota := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&InvitationCode{}).Where("id = ?", code.Id).
			Updates(map[string]interface{}{
				"used_user_id": userId,
				"used_time":    common.GetTimestamp(),
			}).Error
		if err != nil {
			return err
		}
		// 按比例给使用者增加余额
		if code.Quota > 0 && common.InvitationCodeRewardRatio > 0 {
			rewardQuota = code.Quota * common.InvitationCodeRewardRatio / 100
			if rewardQuota > 0 {
				err := tx.Model(&User{}).Where("id = ?", userId).
					Update("quota", gorm.Expr("quota + ?", rewardQuota)).Error
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if rewardQuota > 0 {
		RecordLog(userId, LogTypeTopup, fmt.Sprintf("使用邀请码获得 %s", logger.LogQuota(rewardQuota)))
	}
	return nil
}

// ReleaseInvitationCode 释放已占用但尚未归属用户的邀请码（建号失败时回滚）
func ReleaseInvitationCode(code *InvitationCode) {
	if code == nil {
		return
	}
	err := DB.Model(&InvitationCode{}).
		Where("id = ? AND status = ? AND used_user_id = 0", code.Id, common.InvitationCodeStatusUsed).
		Updates(map[string]interface{}{
			"status":    common.InvitationCodeStatusEnabled,
			"used_time": 0,
		}).Error
	if err != nil {
		common.SysError("failed to release invitation code: " + err.Error())
	}
}

func (code *InvitationCode) Insert() error {
	return DB.Create(code).Error
}

func (code *InvitationCode) Update() error {
	return DB.Model(code).Select("status", "remark").Updates(code).Error
}

func (code *InvitationCode) Delete() error {
	return DB.Delete(code).Error
}

func DeleteInvitationCodeById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	code := InvitationCode{Id: id}
	err := DB.Where(code).First(&code).Error
	if err != nil {
		return err
	}
	return code.Delete()
}

func DeleteUsedInvitationCodes() (int64, error) {
	result := DB.Where("status = ?", common.InvitationCodeStatusUsed).Delete(&InvitationCode{})
	return result.RowsAffected, result.Error
}

func DeleteUsedInvitationCodesByUser(userId int) (int64, error) {
	result := DB.Where("user_id = ? AND status = ?", userId, common.InvitationCodeStatusUsed).Delete(&InvitationCode{})
	return result.RowsAffected, result.Error
}

func DeleteInvitationCodesByUser(userId int, ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("user_id = ? AND id IN ?", userId, ids).Delete(&InvitationCode{})
	return result.RowsAffected, result.Error
}

// GenerateInvitationCodesForUser 为用户批量生成邀请码，扣减额度
func GenerateInvitationCodesForUser(userId int, count int, remark string) ([]*InvitationCode, error) {
	if count <= 0 {
		count = 1
	}
	price := common.InvitationCodePrice
	totalPrice := price * count
	if totalPrice > 0 {
		userQuota, err := GetUserQuota(userId, true)
		if err != nil {
			return nil, err
		}
		if userQuota < totalPrice {
			return nil, errors.New("额度不足")
		}
		err = DecreaseUserQuota(userId, totalPrice, true)
		if err != nil {
			return nil, err
		}
	}

	codes := make([]*InvitationCode, 0, count)
	createdIDs := make([]int, 0, count)
	for i := 0; i < count; i++ {
		code := &InvitationCode{
			UserId:      userId,
			Code:        common.GetUUID(),
			Quota:       price, // 记录每个邀请码消耗的额度
			Status:      common.InvitationCodeStatusEnabled,
			CreatedTime: common.GetTimestamp(),
			Remark:      remark,
		}
		err := code.Insert()
		if err != nil {
			if len(createdIDs) > 0 {
				_ = DB.Where("user_id = ? AND id IN ?", userId, createdIDs).Delete(&InvitationCode{}).Error
			}
			if totalPrice > 0 {
				_ = IncreaseUserQuota(userId, totalPrice, true)
			}
			return nil, err
		}
		codes = append(codes, code)
		createdIDs = append(createdIDs, code.Id)
	}

	if totalPrice > 0 {
		RecordLog(userId, LogTypeSystem, fmt.Sprintf("生成邀请码消耗 %s", logger.LogQuota(totalPrice)))
	}
	return codes, nil
}

// AttachInvitationInfo 为用户列表填充邀请人用户名与注册时使用的邀请码（管理端展示用）
func AttachInvitationInfo(users []*User) {
	if len(users) == 0 {
		return
	}
	userIds := make([]int, 0, len(users))
	inviterIds := make([]int, 0)
	for _, u := range users {
		userIds = append(userIds, u.Id)
		if u.InviterId != 0 {
			inviterIds = append(inviterIds, u.InviterId)
		}
	}
	if len(inviterIds) > 0 {
		var inviters []User
		if err := ReadDB().Unscoped().Select("id", "username").Where("id IN ?", inviterIds).Find(&inviters).Error; err == nil {
			nameById := make(map[int]string, len(inviters))
			for _, inviter := range inviters {
				nameById[inviter.Id] = inviter.Username
			}
			for _, u := range users {
				if u.InviterId != 0 {
					u.InviterName = nameById[u.InviterId]
				}
			}
		}
	}
	var codes []InvitationCode
	if err := ReadDB().Select("code", "used_user_id").Where("used_user_id IN ?", userIds).Find(&codes).Error; err == nil {
		codeByUser := make(map[int]string, len(codes))
		for _, code := range codes {
			codeByUser[code.UsedUserId] = code.Code
		}
		for _, u := range users {
			u.UsedInvitationCode = codeByUser[u.Id]
		}
	}
}

// GenerateInvitationCodeForUser 为用户生成单个邀请码，扣减额度
func GenerateInvitationCodeForUser(userId int, remark string) (*InvitationCode, error) {
	codes, err := GenerateInvitationCodesForUser(userId, 1, remark)
	if err != nil {
		return nil, err
	}
	return codes[0], nil
}
