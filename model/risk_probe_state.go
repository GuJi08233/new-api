package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// RiskProbeUserState 记录单轮内容测活防护的跨节点账号窗口与违规状态。
// 普通请求在内存中即快速放行，只有命中测活规则的请求才会读写此表。
type RiskProbeUserState struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	UserId         int    `json:"user_id" gorm:"uniqueIndex:uidx_risk_probe_user_states_user_id;not null"`
	OffenseCount   int    `json:"offense_count" gorm:"not null;default:0"`
	LastOffenseAt  int64  `json:"last_offense_at" gorm:"bigint;not null;default:0"`
	LastEventAt    int64  `json:"-" gorm:"bigint;not null;default:0"`
	LastRuleId     string `json:"last_rule_id" gorm:"type:varchar(96)"`
	LastRuleSample string `json:"last_rule_sample" gorm:"type:varchar(255)"`
	LastRequestId  string `json:"last_request_id" gorm:"type:varchar(128)"`
	PendingBatch   string `json:"-" gorm:"type:text"`
}

func (RiskProbeUserState) TableName() string {
	return "risk_probe_user_states"
}

type RiskProbeBatchPolicy struct {
	WindowSeconds int
	Threshold     int
	RecordOffense bool // 演练或仅告警模式为 false，避免空转拉高违规计数
}

type RiskProbeBatchResult struct {
	User             User
	State            RiskProbeUserState
	Ignored          bool
	ThresholdReached bool
	MatchedRequests  int
}

type riskProbePendingBatch struct {
	WindowSeconds int     `json:"window_seconds"`
	Threshold     int     `json:"threshold"`
	RecordOffense bool    `json:"record_offense"`
	Requests      []int64 `json:"requests"`
}

// RecordRiskProbePayloadMatch 在主库事务内原子更新账号的单轮测活滑窗，
// 保证多节点并发命中准确计数，并过滤早于上次触发/重置的在途并发请求。
func RecordRiskProbePayloadMatch(userId int, requestStarted time.Time, ruleId, ruleSample, requestId string, policy RiskProbeBatchPolicy) (*RiskProbeBatchResult, error) {
	return recordRiskProbePayloadMatchAt(userId, requestStarted, ruleId, ruleSample, requestId, policy, time.Time{})
}

func recordRiskProbePayloadMatchAt(userId int, requestStarted time.Time, ruleId, ruleSample, requestId string, policy RiskProbeBatchPolicy, now time.Time) (*RiskProbeBatchResult, error) {
	if policy.WindowSeconds < 1 || policy.WindowSeconds > 3600 || policy.Threshold < 1 || policy.Threshold > 1000 {
		return nil, errors.New("invalid probe payload batch policy")
	}
	result := &RiskProbeBatchResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Omit("password").First(&result.User, "id = ?", userId).Error; err != nil {
			return err
		}
		if result.User.Role >= common.RoleAdminUser || result.User.Status != common.UserStatusEnabled {
			return nil
		}
		err := tx.Where("user_id = ?", userId).First(&result.State).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// 早于上次触发或重置时刻发起的在途并发请求不再累计或二次升级。
		if !requestStarted.IsZero() && requestStarted.UnixNano() <= result.State.LastEventAt {
			result.Ignored = true
			return nil
		}
		if now.IsZero() {
			now = time.Now()
		}
		recent := riskProbePendingBatch{}
		if result.State.PendingBatch != "" {
			if err := common.UnmarshalJsonStr(result.State.PendingBatch, &recent); err != nil {
				recent = riskProbePendingBatch{}
			}
		}
		if recent.WindowSeconds != policy.WindowSeconds || recent.Threshold != policy.Threshold || recent.RecordOffense != policy.RecordOffense {
			recent = riskProbePendingBatch{
				WindowSeconds: policy.WindowSeconds,
				Threshold:     policy.Threshold,
				RecordOffense: policy.RecordOffense,
			}
		}
		cutoff := now.Add(-time.Duration(policy.WindowSeconds) * time.Second).UnixNano()
		active := recent.Requests[:0]
		for _, seen := range recent.Requests {
			if seen > cutoff {
				active = append(active, seen)
			}
		}
		recent.Requests = active
		seen := now.UnixNano()
		if !requestStarted.IsZero() && requestStarted.Before(now) {
			seen = requestStarted.UnixNano()
		}
		if seen > cutoff {
			recent.Requests = append(recent.Requests, seen)
		}
		result.MatchedRequests = len(recent.Requests)
		result.State.UserId = userId
		if result.MatchedRequests < policy.Threshold {
			data, err := common.Marshal(recent)
			if err != nil {
				return err
			}
			result.State.PendingBatch = string(data)
			return tx.Save(&result.State).Error
		}

		result.ThresholdReached = true
		result.State.PendingBatch = ""
		result.State.LastEventAt = now.UnixNano()
		result.State.LastRuleId = truncateRiskField(ruleId, 96)
		result.State.LastRuleSample = truncateRiskField(ruleSample, 255)
		result.State.LastRequestId = truncateRiskField(requestId, 128)
		if policy.RecordOffense {
			result.State.OffenseCount++
			result.State.LastOffenseAt = now.Unix()
		}
		return tx.Save(&result.State).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetRiskProbeUserState 读取账号的单轮内容测活累计状态；尚无记录时返回零值结构。
func GetRiskProbeUserState(userId int) (*RiskProbeUserState, error) {
	if userId <= 0 {
		return nil, errors.New("无效的用户 ID")
	}
	state := &RiskProbeUserState{UserId: userId}
	err := DB.Where("user_id = ?", userId).First(state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

// ResetRiskProbeUserState 清零账号的单轮内容测活计数与待定窗口，不改动账号当前的启禁用状态。
func ResetRiskProbeUserState(userId int) error {
	if userId <= 0 {
		return errors.New("无效的用户 ID")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").First(&user, "id = ?", userId).Error; err != nil {
			return err
		}
		state := RiskProbeUserState{UserId: userId}
		if err := tx.Where("user_id = ?", userId).First(&state).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state.OffenseCount = 0
		state.LastOffenseAt = 0
		state.LastEventAt = time.Now().UnixNano()
		state.PendingBatch = ""
		state.LastRuleId = ""
		state.LastRuleSample = ""
		state.LastRequestId = ""
		return tx.Save(&state).Error
	})
}
