package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type Midjourney struct {
	RefundPending    bool   `json:"-"`
	TokenId          int    `json:"-"`
	BillingChannelId int    `json:"-"`
	Id               int    `json:"id"`
	Code             int    `json:"code"`
	UserId           int    `json:"user_id" gorm:"index"`
	Action           string `json:"action" gorm:"type:varchar(40);index"`
	MjId             string `json:"mj_id" gorm:"index"`
	Prompt           string `json:"prompt"`
	PromptEn         string `json:"prompt_en"`
	Description      string `json:"description"`
	State            string `json:"state"`
	SubmitTime       int64  `json:"submit_time" gorm:"index"`
	StartTime        int64  `json:"start_time" gorm:"index"`
	FinishTime       int64  `json:"finish_time" gorm:"index"`
	ImageUrl         string `json:"image_url"`
	VideoUrl         string `json:"video_url"`
	VideoUrls        string `json:"video_urls"`
	Status           string `json:"status" gorm:"type:varchar(20);index"`
	Progress         string `json:"progress" gorm:"type:varchar(30);index"`
	FailReason       string `json:"fail_reason"`
	ChannelId        int    `json:"channel_id"`
	Quota            int    `json:"quota"`
	Buttons          string `json:"buttons"`
	Properties       string `json:"properties"`
	// 提交任务的用户来源。构图失败退款发生在回调/轮询阶段，那时的请求来自上游而非用户，
	// 因此来源必须在提交时快照下来，退款日志才指向真正的发起者。
	// 不对外序列化：与任务本身无关，只供内部日志归属使用。
	SubmitIp string `json:"-" gorm:"type:varchar(64);default:''"`
	SubmitUa string `json:"-" gorm:"type:varchar(512);default:''"`
}

// LogSource 返回提交任务时快照下来的来源，供异步退款日志使用。
func (midjourney *Midjourney) LogSource() LogSource {
	return LogSource{Ip: midjourney.SubmitIp, Ua: midjourney.SubmitUa}
}

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := ReadDB().Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := ReadDB()

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllUnFinishTasks() []*Midjourney {
	var tasks []*Midjourney
	var err error
	// get all tasks progress is not 100%
	err = DB.Where("progress != ? OR refund_pending = ?", "100%", true).Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedMidjourneyTasks reports whether at least one Midjourney task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the midjourney_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func HasUnfinishedMidjourneyTasks() bool {
	var id int
	err := DB.Model(&Midjourney{}).
		Where("progress != ? OR refund_pending = ?", "100%", true).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func GetByMJId(userId int, mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = ReadDB().Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJIds(userId int, mjIds []string) []*Midjourney {
	var mj []*Midjourney
	var err error
	err = ReadDB().Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = ReadDB().Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func UpdateProgress(id int, progress string) error {
	return DB.Model(&Midjourney{}).Where("id = ?", id).Update("progress", progress).Error
}

func (midjourney *Midjourney) Insert() error {
	var err error
	err = DB.Create(midjourney).Error
	return err
}

func (midjourney *Midjourney) Update() error {
	var err error
	err = DB.Save(midjourney).Error
	return err
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (midjourney *Midjourney) UpdateWithStatus(fromStatus string) (bool, error) {
	result := DB.Model(midjourney).Where("status = ?", fromStatus).Select("*").Omit("quota", "token_id", "billing_channel_id").Updates(midjourney)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func MjBulkUpdate(mjIds []string, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("mj_id in (?)", mjIds).
		Updates(params).Error
}

func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	return DB.Model(&Midjourney{}).
		Where("id in (?)", taskIDs).
		Updates(params).Error
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := ReadDB().Model(&Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := ReadDB().Model(&Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

func (m *Midjourney) UpdateBillingState() error {
	return DB.Model(m).Select("quota", "token_id", "billing_channel_id").Updates(m).Error
}

// ChargeMidjourneyTask 将钱包、令牌和退款标记原子提交，失败则任务不会留下假退款额度。
func ChargeMidjourneyTask(task *Midjourney, quota, tokenID, billingChannelID int, tokenKey string) error {
	if task == nil || task.Id == 0 || quota <= 0 || quota >= common.MaxQuota {
		return ErrQuotaOutOfRange
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Midjourney
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.Quota != 0 {
			return errors.New("Midjourney task already billed")
		}
		if current.Status == "FAILURE" {
			return errors.New("Midjourney task already failed")
		}
		result := tx.Model(&User{}).Where("id = ? AND quota >= ?", task.UserId, common.MinQuota+1+quota).Update("quota", gorm.Expr("quota - ?", quota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQuotaOutOfRange
		}
		if tokenID > 0 {
			result = tx.Model(&Token{}).Where("id = ? AND remain_quota >= ? AND used_quota <= ?", tokenID, common.MinQuota+1+quota, common.MaxQuota-1-quota).Updates(map[string]interface{}{"remain_quota": gorm.Expr("remain_quota - ?", quota), "used_quota": gorm.Expr("used_quota + ?", quota), "accessed_time": common.GetTimestamp()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrQuotaOutOfRange
			}
		}
		result = tx.Model(&Midjourney{}).Where("id = ? AND quota = ? AND status != ?", task.Id, 0, "FAILURE").Updates(map[string]interface{}{"quota": quota, "token_id": tokenID, "billing_channel_id": billingChannelID})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Midjourney billing state changed")
		}
		return nil
	})
	if err != nil {
		return err
	}
	task.Quota, task.TokenId, task.BillingChannelId = quota, tokenID, billingChannelID
	syncCreditUserQuotaCache(task.UserId, -quota, "midjourney")
	if err := cacheDeleteToken(tokenKey); err != nil {
		common.SysError("failed to invalidate Midjourney token cache: " + err.Error())
	}
	return nil
}

// LoadMidjourneyBillingState 读取收费事务提交后的退款标记，轮询快照可能早于收费。
func (task *Midjourney) LoadBillingState() error {
	var current Midjourney
	if err := DB.Select("quota", "token_id", "billing_channel_id").First(&current, task.Id).Error; err != nil {
		return err
	}
	task.Quota, task.TokenId, task.BillingChannelId = current.Quota, current.TokenId, current.BillingChannelId
	return nil
}
