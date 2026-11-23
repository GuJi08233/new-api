package setting

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const MaxModelRateLimitTokens int64 = 1<<53 - 1

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitTokenCount int64 = 0
var ModelRequestRateLimitConcurrency = 0
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitGroupTokens = map[string]int64{}
var ModelRequestRateLimitGroupConcurrency = map[string]int{}
var ModelRequestRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	encoded := make(map[string][4]int64, len(ModelRequestRateLimitGroup))
	for group, limits := range ModelRequestRateLimitGroup {
		encoded[group] = [4]int64{
			int64(limits[0]),
			int64(limits[1]),
			ModelRequestRateLimitGroupTokens[group],
			int64(ModelRequestRateLimitGroupConcurrency[group]),
		}
	}

	jsonBytes, err := common.Marshal(encoded)
	if err != nil {
		common.SysLog("error marshalling model rate limit group: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

// NormalizeModelRequestRateLimitGroupJSON upgrades legacy 2-element or 3-element
// group entries to the full 4-element form [total, success, tokens, concurrency],
// defaulting omitted TPM and concurrency fields to 0.
func NormalizeModelRequestRateLimitGroupJSON(jsonStr string) (string, bool, error) {
	rawGroups := make(map[string][]int64)
	if err := common.Unmarshal([]byte(jsonStr), &rawGroups); err != nil {
		return "", false, err
	}

	changed := false
	normalized := make(map[string][4]int64, len(rawGroups))
	for group, limits := range rawGroups {
		if len(limits) < 2 || len(limits) > 4 {
			return "", false, fmt.Errorf("group %s must have 2 to 4 rate limit values", group)
		}
		var entry [4]int64
		entry[0] = limits[0]
		entry[1] = limits[1]
		if len(limits) >= 3 {
			entry[2] = limits[2]
		}
		if len(limits) >= 4 {
			entry[3] = limits[3]
		}
		if len(limits) < 4 {
			changed = true
		}
		normalized[group] = entry
	}
	if !changed {
		return jsonStr, false, nil
	}
	encoded, err := common.Marshal(normalized)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	rawGroups := make(map[string][]int64)
	if err := common.Unmarshal([]byte(jsonStr), &rawGroups); err != nil {
		return err
	}

	nextGroups := make(map[string][2]int, len(rawGroups))
	nextTokenLimits := make(map[string]int64)
	nextConcurrencyLimits := make(map[string]int)
	for group, limits := range rawGroups {
		if len(limits) < 2 || len(limits) > 4 {
			return fmt.Errorf("group %s must have 2 to 4 rate limit values", group)
		}
		nextGroups[group] = [2]int{int(limits[0]), int(limits[1])}
		if len(limits) >= 3 && limits[2] > 0 {
			nextTokenLimits[group] = limits[2]
		}
		if len(limits) >= 4 && limits[3] > 0 {
			nextConcurrencyLimits[group] = int(limits[3])
		}
	}

	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()
	ModelRequestRateLimitGroup = nextGroups
	ModelRequestRateLimitGroupTokens = nextTokenLimits
	ModelRequestRateLimitGroupConcurrency = nextConcurrencyLimits
	return nil
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	totalCount, successCount, _, _, found = GetGroupRateLimitConfig(group)
	return totalCount, successCount, found
}

func GetGroupRateLimitConfig(group string) (totalCount, successCount int, tokenCount int64, concurrencyLimit int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, 0, 0, false
	}
	return limits[0], limits[1], ModelRequestRateLimitGroupTokens[group], ModelRequestRateLimitGroupConcurrency[group], true
}

func CheckModelRequestRateLimitTokenCount(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("ModelRequestRateLimitTokenCount must be a non-negative integer")
	}
	val, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return fmt.Errorf("ModelRequestRateLimitTokenCount must be an integer: %w", err)
	}
	if val < 0 || val > MaxModelRateLimitTokens {
		return fmt.Errorf("ModelRequestRateLimitTokenCount must be between 0 and %d", MaxModelRateLimitTokens)
	}
	return nil
}

func CheckModelRequestRateLimitConcurrency(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("ModelRequestRateLimitConcurrency must be a non-negative integer")
	}
	val, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return fmt.Errorf("ModelRequestRateLimitConcurrency must be an integer: %w", err)
	}
	if val < 0 || val > math.MaxInt32 {
		return fmt.Errorf("ModelRequestRateLimitConcurrency must be between 0 and %d", math.MaxInt32)
	}
	return nil
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][]int64)
	err := common.Unmarshal([]byte(jsonStr), &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if len(limits) < 2 || len(limits) > 4 {
			return fmt.Errorf("group %s must have 2 to 4 rate limit values: [totalCount, successCount(, tokenCount, concurrencyLimit)]", group)
		}
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
		if len(limits) >= 3 && (limits[2] < 0 || limits[2] > MaxModelRateLimitTokens) {
			return fmt.Errorf("group %s token rate limit %d must be between 0 and %d", group, limits[2], MaxModelRateLimitTokens)
		}
		if len(limits) >= 4 && (limits[3] < 0 || limits[3] > math.MaxInt32) {
			return fmt.Errorf("group %s concurrency limit %d must be between 0 and %d", group, limits[3], math.MaxInt32)
		}
	}

	return nil
}
