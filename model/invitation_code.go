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
	Id           int            `json:"id"`
	UserId       int            `json:"user_id" gorm:"index"`                               // 生成者 ID
	Code         string         `json:"code" gorm:"type:varchar(32);uniqueIndex"`           // 邀请码
	Quota        int            `json:"quota" gorm:"default:0"`                             // 单次使用消耗的额度基数(奖励按此计算)
	Status       int            `json:"status" gorm:"default:1"`                            // 1=未使用, 2=已使用, 3=已禁用
	UsedUserId   int            `json:"used_user_id"`                                       // 最后一次使用者 ID
	UsedType     int            `json:"used_type" gorm:"default:0"`                         // 使用用途：1=注册，2=兑换；0=未使用或历史数据
	UsedTime     int64          `json:"used_time" gorm:"bigint"`                            // 最后一次使用时间
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`                         // 创建时间
	Count        int            `json:"count" gorm:"-:all"`                                 // 仅用于批量创建请求
	MaxUses      int            `json:"max_uses"`                                           // 可使用次数,0/1 均为单次(兼容旧数据)
	UsedCount    int            `json:"used_count"`                                         // 已使用次数
	Remark       string         `json:"remark" gorm:"type:varchar(255)" validate:"max=255"` // 备注
	UserName     string         `json:"user_name,omitempty" gorm:"-:all"`                   // 生成者用户名，仅管理端列表展示
	UsedUserName string         `json:"used_user_name,omitempty" gorm:"-:all"`              // 使用者用户名，仅管理端列表展示
	DeletedAt    gorm.DeletedAt `gorm:"index"`
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

// ReserveInvitationCode 在创建用户之前原子占用邀请码的一次使用名额,
// 防止并发注册用同一个码同时通过校验。单次码占用后状态直接置为已使用,
// 多用码递增 used_count、用满才关闭。占用成功后必须调用
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
		maxUses := resolveMaxUses(code.MaxUses)
		// CAS 递增占用次数,SQLite 等无行锁场景下并发注册同样不会超占
		result := tx.Model(&InvitationCode{}).
			Where("id = ? AND status = ? AND used_count < ?", code.Id, common.InvitationCodeStatusEnabled, maxUses).
			Updates(map[string]interface{}{
				"used_count": gorm.Expr("used_count + 1"),
				"used_time":  common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInvitationCodeNotUsable
		}
		// 名额用满后关闭该码(幂等)
		return tx.Model(&InvitationCode{}).
			Where("id = ? AND status = ? AND used_count >= ?", code.Id, common.InvitationCodeStatusEnabled, maxUses).
			Update("status", common.InvitationCodeStatusUsed).Error
	})
	if err != nil {
		return nil, err
	}
	return &code, nil
}

// FinalizeInvitationCodeUsage 将已占用的邀请码归属到注册成功的用户。
// 注册使用只建立邀请关系,不发放兑换奖励;按比例的奖励额度只在兑换路径(Redeem)发放。
func FinalizeInvitationCodeUsage(code *InvitationCode, userId int) error {
	if code == nil {
		return nil
	}
	if userId == 0 {
		return errors.New("无效的 user id")
	}
	return DB.Model(&InvitationCode{}).Where("id = ?", code.Id).
		Updates(map[string]interface{}{
			"used_user_id": userId,
			"used_type":    common.InvitationCodeUsedTypeRegister,
			"used_time":    common.GetTimestamp(),
		}).Error
}

// ReleaseInvitationCode 释放已占用但尚未归属用户的名额（建号失败时回滚）:
// 回退 used_count 并重新打开该码。与占用时相同,以 used_user_id 未归属为前提,
// 多用码在并发注册且他人已归属时可能少回退一个名额,属可接受的边缘情况。
func ReleaseInvitationCode(code *InvitationCode) {
	if code == nil {
		return
	}
	err := DB.Model(&InvitationCode{}).
		Where("id = ? AND used_user_id = 0 AND used_count > 0", code.Id).
		Updates(map[string]interface{}{
			"used_count": gorm.Expr("used_count - 1"),
			"status":     common.InvitationCodeStatusEnabled,
			"used_time":  0,
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

// GenerateInvitationCodesForUser 为用户批量生成邀请码，扣减额度。
// maxUses 为每个码的可使用次数,一码多用按次数计价(总价 = 单价 × 数量 × 次数),
// Quota 字段仍记录单次基数,使用者每次按该基数领取奖励。
func GenerateInvitationCodesForUser(source LogSource, userId int, count int, maxUses int, remark string) ([]*InvitationCode, error) {
	if count <= 0 {
		count = 1
	}
	maxUses = resolveMaxUses(maxUses)
	price := common.InvitationCodePrice
	totalPrice := price * count * maxUses
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
			Quota:       price, // 记录单次使用的额度基数
			Status:      common.InvitationCodeStatusEnabled,
			CreatedTime: common.GetTimestamp(),
			MaxUses:     maxUses,
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
		RecordLog(source, userId, LogTypeSystem, fmt.Sprintf("生成邀请码消耗 %s", logger.LogQuota(totalPrice)))
	}
	return codes, nil
}

// AttachInvitationCodeUsers 为邀请码列表填充生成者与使用者用户名（管理端展示用）
func AttachInvitationCodeUsers(codes []*InvitationCode) {
	if len(codes) == 0 {
		return
	}
	idSet := make(map[int]struct{})
	for _, c := range codes {
		if c.UserId != 0 {
			idSet[c.UserId] = struct{}{}
		}
		if c.UsedUserId != 0 {
			idSet[c.UsedUserId] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var users []User
	if err := ReadDB().Unscoped().Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return
	}
	nameById := make(map[int]string, len(users))
	for _, u := range users {
		nameById[u.Id] = u.Username
	}
	for _, c := range codes {
		c.UserName = nameById[c.UserId]
		if c.UsedUserId != 0 {
			c.UsedUserName = nameById[c.UsedUserId]
		}
	}
}

// AttachInvitationInfo 为用户列表填充邀请人用户名与注册时使用的邀请码（管理端展示用）
func AttachInvitationInfo(users []*User) {
	if len(users) == 0 {
		return
	}
	userIds := make([]int, 0, len(users))
	for _, u := range users {
		userIds = append(userIds, u.Id)
	}
	// 只按注册使用反查：兑换核销（used_type=2）不产生邀请关系；
	// 历史数据（used_type=0）按"之前的不管"约定同样排除
	var codes []InvitationCode
	if err := ReadDB().Select("code", "user_id", "used_user_id").
		Where("used_user_id IN ? AND used_type = ?", userIds, common.InvitationCodeUsedTypeRegister).
		Find(&codes).Error; err == nil {
		codeByUser := make(map[int]*InvitationCode, len(codes))
		for i := range codes {
			codeByUser[codes[i].UsedUserId] = &codes[i]
		}
		for _, u := range users {
			code, ok := codeByUser[u.Id]
			if !ok {
				continue
			}
			u.UsedInvitationCode = code.Code
			// 历史 OAuth 注册未落库 inviter_id 时，按邀请码归属补出邀请人
			if u.InviterId == 0 {
				u.InviterId = code.UserId
			}
		}
	}
	inviterIds := make([]int, 0)
	for _, u := range users {
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
}

// GenerateInvitationCodeForUser 为用户生成单个单次可用的邀请码，扣减额度
func GenerateInvitationCodeForUser(source LogSource, userId int, remark string) (*InvitationCode, error) {
	codes, err := GenerateInvitationCodesForUser(source, userId, 1, 1, remark)
	if err != nil {
		return nil, err
	}
	return codes[0], nil
}
