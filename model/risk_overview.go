package model

import "github.com/QuantumNous/new-api/common"

// RiskScanRun 记录主节点每轮扫描的真实执行结果，其他节点的管理页面也能读取。
type RiskScanRun struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Node        string `json:"node" gorm:"type:varchar(128)"`
	StartedAt   int64  `json:"started_at" gorm:"bigint;index:idx_risk_scan_runs_started_at"`
	CompletedAt int64  `json:"completed_at" gorm:"bigint"`
	Status      string `json:"status" gorm:"type:varchar(16)"`
	Rules       int    `json:"rules"`
	Hits        int    `json:"hits"`
	Error       string `json:"error" gorm:"type:text"`
}

func StartRiskScan(node string, rules int) (*RiskScanRun, error) {
	run := &RiskScanRun{Node: truncateRiskField(node, 128), StartedAt: common.GetTimestamp(), Status: "running", Rules: rules}
	return run, DB.Create(run).Error
}

func FinishRiskScan(run *RiskScanRun, hits int, scanErr error) error {
	status, message := "success", ""
	if scanErr != nil {
		status, message = "failed", scanErr.Error()
	}
	return DB.Model(&RiskScanRun{}).Where("id = ?", run.Id).Updates(map[string]interface{}{
		"completed_at": common.GetTimestamp(), "status": status, "hits": hits, "error": message,
	}).Error
}

func GetLatestRiskScan() (*RiskScanRun, error) {
	var runs []RiskScanRun
	if err := DB.Order("id desc").Limit(1).Find(&runs).Error; err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

type RiskDispositionCounts struct {
	DisabledUsers int64 `json:"disabled_users"`
	ActiveIpBans  int64 `json:"active_ip_bans"`
	PendingAlerts int64 `json:"pending_alerts"`
}

func GetRiskDispositionCounts() (*RiskDispositionCounts, error) {
	counts := &RiskDispositionCounts{}
	if err := DB.Model(&User{}).Where("status = ?", common.UserStatusDisabled).Count(&counts.DisabledUsers).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&IpBan{}).Where("expires_at = 0 OR expires_at > ?", common.GetTimestamp()).Count(&counts.ActiveIpBans).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&RiskEvent{}).Where("event_type = ?", RiskEventAlert).
		Where("COALESCE(review_status, '') IN ?", []string{"", RiskReviewPending}).Count(&counts.PendingAlerts).Error; err != nil {
		return nil, err
	}
	return counts, nil
}

type RiskEventTotal struct {
	EventType string `json:"event_type"`
	Records   int64  `json:"records"`
	Hits      int64  `json:"hits"`
}

func GetRecentRiskEventTotals(since int64) ([]RiskEventTotal, error) {
	var totals []RiskEventTotal
	err := DB.Model(&RiskEvent{}).Select("event_type, count(*) as records, sum(count) as hits").
		Where("created_at >= ?", since).Group("event_type").Find(&totals).Error
	return totals, err
}

type RiskLogCoverage struct {
	Requests int64 `json:"requests"`
	WithIp   int64 `json:"with_ip"`
	WithUa   int64 `json:"with_ua"`
}

func GetRiskLogCoverage(since int64) (*RiskLogCoverage, error) {
	coverage := &RiskLogCoverage{}
	err := LOG_DB.Model(&Log{}).Select("count(*) as requests, count(case when ip <> '' then 1 end) as with_ip, count(case when ua <> '' then 1 end) as with_ua").
		Where("created_at >= ?", since).Where("type IN ?", riskLogTypes()).Scan(coverage).Error
	return coverage, err
}
