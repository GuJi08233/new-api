package service

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

type RiskRuntimeStatus struct {
	StartedAt      int64  `json:"started_at"`
	CacheSyncedAt  int64  `json:"cache_synced_at"`
	CacheError     string `json:"cache_error"`
	EventWrittenAt int64  `json:"event_written_at"`
	EventErrorAt   int64  `json:"event_error_at"`
	EventError     string `json:"event_error"`
}

var riskRuntimeMu sync.RWMutex
var riskRuntime RiskRuntimeStatus

func GetRiskRuntimeStatus() RiskRuntimeStatus {
	riskRuntimeMu.RLock()
	defer riskRuntimeMu.RUnlock()
	return riskRuntime
}

func recordRiskCacheSync(err error) {
	riskRuntimeMu.Lock()
	defer riskRuntimeMu.Unlock()
	if err != nil {
		riskRuntime.CacheError = err.Error()
		return
	}
	riskRuntime.CacheSyncedAt = common.GetTimestamp()
	riskRuntime.CacheError = ""
}
