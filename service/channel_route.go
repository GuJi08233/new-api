package service

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const ginKeyChannelRouteLogInfo = "channel_route_log_info"

var channelRouteRegexCache sync.Map // map[string]*regexp.Regexp

type ChannelRouteMatch struct {
	Channel     *model.Channel
	SelectGroup string
	Matched     bool
	Strict      bool
	Exhausted   bool
	RuleName    string
}

func GetChannelByRoute(param *RetryParam) (*ChannelRouteMatch, error) {
	result := &ChannelRouteMatch{}
	if param == nil || param.Ctx == nil || param.ModelName == "" {
		return result, nil
	}

	settingConfig := operation_setting.GetChannelRouteSetting()
	if settingConfig == nil || !settingConfig.Enabled {
		return result, nil
	}

	path := ""
	if param.Ctx.Request != nil && param.Ctx.Request.URL != nil {
		path = param.Ctx.Request.URL.Path
	}
	matchGroup := getChannelRouteMatchGroup(param)

	for _, rule := range settingConfig.Rules {
		if len(rule.GroupRegex) > 0 && !channelRouteMatchAnyRegex(rule.GroupRegex, matchGroup) {
			continue
		}
		if !channelRouteMatchAnyRegex(rule.ModelRegex, param.ModelName) {
			continue
		}
		if len(rule.PathRegex) > 0 && !channelRouteMatchAnyRegex(rule.PathRegex, path) {
			continue
		}

		result.Matched = true
		result.Strict = rule.Strict
		result.RuleName = strings.TrimSpace(rule.Name)

		// Tiered channel routing. Old configurations stored a separate
		// rule.ChannelIDs fallback pool; we surface that as an implicit
		// catch-all tier so legacy data keeps working without a UI for it.
		tiers := resolveRouteTiers(rule)
		var channelIDs []int
		matchedTier := ""
		estimatedTokens := common.GetContextKeyInt(param.Ctx, constant.ContextKeyEstimatedTokens)
		if len(tiers) > 0 {
			if estimatedTokens > 0 {
				for _, tier := range tiers {
					if evaluateRouteTier(tier.Conditions, estimatedTokens) {
						if len(tier.ChannelIDs) > 0 {
							channelIDs = tier.ChannelIDs
							matchedTier = tier.Label
							break
						}
						// ChannelIDs empty, continue to next tier
					}
				}
			} else {
				// estimatedTokens unknown: either the Distribute phase (the
				// Relay controller re-routes precisely once real tokens are
				// known) or a relay path that never estimates tokens (task/
				// Midjourney relays). Prefer catch-all tiers so those paths
				// keep the legacy fallback-pool semantics; only fall back to
				// the union of all tier channels when no catch-all exists.
				channelIDs = collectTierChannelIDs(tiers, true)
				if len(channelIDs) == 0 {
					channelIDs = collectTierChannelIDs(tiers, false)
				}
			}
		}

		channel, selectGroup, exhausted, err := getRouteSatisfiedChannel(param, channelIDs)
		if err != nil {
			return nil, err
		}
		result.Channel = channel
		result.SelectGroup = selectGroup
		result.Exhausted = exhausted

		if channel != nil {
			markChannelRouteUsed(param.Ctx, rule, channelIDs, param.ModelName, param.TokenGroup, selectGroup, channel.Id, path, estimatedTokens, matchedTier, len(tiers))
		} else {
			markChannelRouteExhausted(param.Ctx, rule, channelIDs, param.ModelName, param.TokenGroup, path, estimatedTokens, matchedTier, len(tiers))
		}
		return result, nil
	}

	return result, nil
}

// resolveRouteTiers returns the rule's tier list, transparently appending an
// implicit catch-all tier built from the deprecated rule.ChannelIDs field when
// the rule has no existing catch-all. This lets older stored configurations
// (which relied on a separate fallback pool) keep working after the UI was
// simplified to expose only tiers.
func resolveRouteTiers(rule operation_setting.ChannelRouteRule) []operation_setting.RouteTier {
	if len(rule.ChannelIDs) == 0 {
		return rule.RouteTiers
	}
	for _, tier := range rule.RouteTiers {
		if len(tier.Conditions) == 0 && len(tier.ChannelIDs) > 0 {
			// already has a usable catch-all; ignore legacy ChannelIDs
			return rule.RouteTiers
		}
	}
	tiers := make([]operation_setting.RouteTier, 0, len(rule.RouteTiers)+1)
	tiers = append(tiers, rule.RouteTiers...)
	tiers = append(tiers, operation_setting.RouteTier{
		ChannelIDs: rule.ChannelIDs,
	})
	return tiers
}

// collectTierChannelIDs returns the de-duplicated channel IDs of the given
// tiers, optionally restricted to catch-all tiers (empty Conditions).
func collectTierChannelIDs(tiers []operation_setting.RouteTier, catchAllOnly bool) []int {
	seen := make(map[int]struct{})
	var channelIDs []int
	for _, tier := range tiers {
		if catchAllOnly && len(tier.Conditions) > 0 {
			continue
		}
		for _, id := range tier.ChannelIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				channelIDs = append(channelIDs, id)
			}
		}
	}
	return channelIDs
}

func evaluateRouteTier(conditions []operation_setting.RouteTierCondition, estimatedTokens int) bool {
	if len(conditions) == 0 {
		return true
	}
	for _, cond := range conditions {
		if !evaluateRouteCondition(cond, estimatedTokens) {
			return false
		}
	}
	return true
}

func evaluateRouteCondition(cond operation_setting.RouteTierCondition, estimatedTokens int) bool {
	var actual int
	switch cond.Var {
	case "len", "p":
		actual = estimatedTokens
	case "c":
		actual = 0 // completion tokens unknown at routing time
	default:
		return false
	}
	switch cond.Op {
	case "<":
		return actual < cond.Value
	case "<=":
		return actual <= cond.Value
	case ">":
		return actual > cond.Value
	case ">=":
		return actual >= cond.Value
	default:
		return false
	}
}

func getChannelRouteMatchGroup(param *RetryParam) string {
	if param == nil || param.Ctx == nil {
		return ""
	}
	usingGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUsingGroup)
	if usingGroup != "" && usingGroup != "auto" {
		return usingGroup
	}
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
	if userGroup != "" {
		return userGroup
	}
	if usingGroup != "" {
		return usingGroup
	}
	return param.TokenGroup
}

func getRouteSatisfiedChannel(param *RetryParam, channelIDs []int) (*model.Channel, string, bool, error) {
	if len(channelIDs) == 0 {
		return nil, param.TokenGroup, true, nil
	}

	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	if param.TokenGroup == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, false, errors.New("auto groups is not enabled")
		}

		autoGroups := GetUserAutoGroup(userGroup)
		startGroupIndex := 0
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			priorityRetry := param.GetRetry()
			if i > startGroupIndex {
				priorityRetry = 0
			}

			channel, err := getRouteRandomSatisfiedChannel(autoGroup, param.ModelName, priorityRetry, channelIDs)
			if err != nil {
				return nil, autoGroup, false, err
			}
			if channel == nil {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				param.SetRetry(0)
				continue
			}

			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			return channel, selectGroup, false, nil
		}
		return nil, selectGroup, true, nil
	}

	channel, err := getRouteRandomSatisfiedChannel(param.TokenGroup, param.ModelName, param.GetRetry(), channelIDs)
	if err != nil {
		return nil, param.TokenGroup, false, err
	}
	if channel == nil {
		return nil, param.TokenGroup, true, nil
	}
	return channel, param.TokenGroup, false, nil
}

func getRouteRandomSatisfiedChannel(group string, modelName string, retry int, channelIDs []int) (*model.Channel, error) {
	channels := collectRouteCandidatesForGroup(group, modelName, channelIDs)
	if len(channels) == 0 {
		return nil, nil
	}
	if len(channels) == 1 {
		return channels[0], nil
	}

	uniquePriorities := make(map[int]bool)
	for _, channel := range channels {
		uniquePriorities[int(channel.GetPriority())] = true
	}

	if len(uniquePriorities) == 0 {
		return nil, nil
	}

	sortedUniquePriorities := make([]int, 0, len(uniquePriorities))
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	targetChannels := make([]*model.Channel, 0, len(channels))
	sumWeight := 0
	for _, channel := range channels {
		if channel.GetPriority() == targetPriority {
			sumWeight += channel.GetWeight()
			targetChannels = append(targetChannels, channel)
		}
	}
	if len(targetChannels) == 0 {
		return nil, nil
	}
	if len(targetChannels) == 1 {
		return targetChannels[0], nil
	}

	smoothingFactor := 1
	smoothingAdjustment := 0
	if sumWeight == 0 {
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		smoothingFactor = 100
	}

	totalWeight := sumWeight * smoothingFactor
	randomWeight := common.GetRandomInt(totalWeight)
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel, nil
		}
	}
	return targetChannels[len(targetChannels)-1], nil
}

func collectRouteCandidatesForGroup(group string, modelName string, channelIDs []int) []*model.Channel {
	if group == "" || modelName == "" || len(channelIDs) == 0 {
		return nil
	}

	candidates := make([]*model.Channel, 0, len(channelIDs))
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}

		channelModel, err := model.CacheGetChannel(channelID)
		if err != nil || channelModel == nil || channelModel.Status != common.ChannelStatusEnabled {
			continue
		}
		if model.IsChannelEnabledForGroupModel(group, modelName, channelID) {
			candidates = append(candidates, channelModel)
		}
	}
	return candidates
}

func channelRouteMatchAnyRegex(patterns []string, value string) bool {
	if len(patterns) == 0 || value == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		compiled, ok := channelRouteRegexCache.Load(pattern)
		if !ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			compiled, _ = channelRouteRegexCache.LoadOrStore(pattern, re)
		}
		if re, ok := compiled.(*regexp.Regexp); ok && re.MatchString(value) {
			return true
		}
	}
	return false
}

func markChannelRouteUsed(c *gin.Context, rule operation_setting.ChannelRouteRule, channelIDs []int, modelName string, usingGroup string, selectedGroup string, channelID int, requestPath string, estimatedTokens int, matchedTier string, tierCount int) {
	if c == nil {
		return
	}
	logInfo := gin.H{
		"rule_name":      strings.TrimSpace(rule.Name),
		"model":          modelName,
		"request_path":   requestPath,
		"using_group":    usingGroup,
		"selected_group": selectedGroup,
		"channel_ids":    channelIDs,
		"channel_id":     channelID,
		"strict":         rule.Strict,
	}
	if matchedTier != "" {
		logInfo["matched_tier"] = matchedTier
	}
	if tierCount > 0 {
		logInfo["estimated_tokens"] = estimatedTokens
		logInfo["route_tiers"] = tierCount
	}
	c.Set(ginKeyChannelRouteLogInfo, logInfo)
}

func markChannelRouteExhausted(c *gin.Context, rule operation_setting.ChannelRouteRule, channelIDs []int, modelName string, usingGroup string, requestPath string, estimatedTokens int, matchedTier string, tierCount int) {
	if c == nil {
		return
	}
	logInfo := gin.H{
		"rule_name":    strings.TrimSpace(rule.Name),
		"model":        modelName,
		"request_path": requestPath,
		"using_group":  usingGroup,
		"channel_ids":  channelIDs,
		"strict":       rule.Strict,
		"exhausted":    true,
	}
	if matchedTier != "" {
		logInfo["matched_tier"] = matchedTier
	}
	if tierCount > 0 {
		logInfo["estimated_tokens"] = estimatedTokens
		logInfo["route_tiers"] = tierCount
	}
	c.Set(ginKeyChannelRouteLogInfo, logInfo)
}

func AppendChannelRouteAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	if anyInfo, ok := c.Get(ginKeyChannelRouteLogInfo); ok && anyInfo != nil {
		adminInfo["channel_route"] = anyInfo
	}
}
