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
	err := DB.First(&code, "id = ?", id).Error
	return &code, err
}

func GetInvitationCodeByCode(codeStr string) (*InvitationCode, error) {
	if codeStr == "" {
		return nil, errors.New("邀请码为空")
	}
	var code InvitationCode
	err := DB.Where("code = ?", codeStr).First(&code).Error
	if err != nil {
		return nil, err
	}
	return &code, nil
}

func UseInvitationCode(codeStr string, userId int) error {
	if codeStr == "" {
		return errors.New("邀请码为空")
	}
	if userId == 0 {
		return errors.New("无效的 user id")
	}

	common.RandomSleep()
	return DB.Transaction(func(tx *gorm.DB) error {
		var code InvitationCode
		err := lockForUpdate(tx).Where("code = ?", codeStr).First(&code).Error
		if err != nil {
			return errors.New("无效的邀请码")
		}
		if code.Status != common.InvitationCodeStatusEnabled {
			return errors.New("该邀请码已被使用或已禁用")
		}
		// 不能使用自己生成的邀请码
		if code.UserId == userId {
			return errors.New("不能使用自己生成的邀请码")
		}
		// 按比例给使用者增加余额
		if code.Quota > 0 && common.InvitationCodeRewardRatio > 0 {
			rewardQuota := code.Quota * common.InvitationCodeRewardRatio / 100
			if rewardQuota > 0 {
				err := tx.Model(&User{}).Where("id = ?", userId).
					Update("quota", gorm.Expr("quota + ?", rewardQuota)).Error
				if err != nil {
					return err
				}
				RecordLog(userId, LogTypeTopup, fmt.Sprintf("使用邀请码获得 %s", logger.LogQuota(rewardQuota)))
			}
		}
		code.Status = common.InvitationCodeStatusUsed
		code.UsedUserId = userId
		code.UsedTime = common.GetTimestamp()
		return tx.Save(&code).Error
	})
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

// GenerateInvitationCodeForUser 为用户生成单个邀请码，扣减额度
func GenerateInvitationCodeForUser(userId int, remark string) (*InvitationCode, error) {
	codes, err := GenerateInvitationCodesForUser(userId, 1, remark)
	if err != nil {
		return nil, err
	}
	return codes[0], nil
}
