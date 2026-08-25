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
	// Deferred means at least one earlier tier could not be evaluated because a
	// required metric was unavailable. Callers must not enforce strict failure
	// until the route is evaluated again with authoritative request metrics.
	Deferred bool
	// NeedsReroute marks a provisional selection that must be re-evaluated after
	// request parsing/token estimation.
	NeedsReroute bool
	// Tiered distinguishes a real tier configuration from an otherwise empty
	// matching rule so post-estimation fallback does not replace a pinned channel.
	Tiered        bool
	Rejected      bool
	RejectMessage string
	RuleName      string
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
		if len(rule.PathRegex) > 0 && !channelRouteMatchPathRegex(rule.PathRegex, path) {
			continue
		}

		result.Matched = true
		result.Strict = rule.Strict
		result.RuleName = strings.TrimSpace(rule.Name)

		// Tiered channel routing. Old configurations stored a separate
		// rule.ChannelIDs fallback pool; we surface that as an implicit
		// catch-all tier so legacy data keeps working without a UI for it.
		tiers := resolveRouteTiers(rule)
		result.Tiered = hasUsableRouteTier(tiers)
		var channelIDs []int
		matchedTier := ""
		unknownConditions := false
		estimatedTokens, tokensKnown := getRouteMetric(param.Ctx, constant.ContextKeyEstimatedTokens)
		estimatedDocs, docsKnown := getRouteMetric(param.Ctx, constant.ContextKeyEstimatedDocs)
		if len(tiers) > 0 {
			conditionsEvaluated := false
			for _, tier := range tiers {
				// Never treat an unavailable metric as zero. This is required for
				// mixed token/docs rules and for explicit docs=0 requests.
				if !routeTierConditionsKnown(tier.Conditions, tokensKnown, docsKnown) {
					unknownConditions = true
					continue
				}
				conditionsEvaluated = true
				if !evaluateRouteTier(tier.Conditions, estimatedTokens, estimatedDocs) {
					continue
				}
				if tier.Reject {
					// First-match-wins cannot be finalized while an earlier tier is
					// still unknown. Defer this provisional reject until rerouting.
					if unknownConditions {
						break
					}
					result.Rejected = true
					result.RejectMessage = strings.TrimSpace(tier.RejectMessage)
					matchedTier = tier.Label
					markChannelRouteExhausted(param.Ctx, rule, channelIDs, param.ModelName, param.TokenGroup, path, estimatedTokens, matchedTier, len(tiers))
					return result, nil
				}
				if len(tier.ChannelIDs) > 0 {
					channelIDs = tier.ChannelIDs
					matchedTier = tier.Label
					break
				}
				// An empty non-reject tier is ignored so a later usable tier can match.
			}
			// If no tier was evaluable during the early distributor pass, use a
			// provisional pool. Prefer a catch-all pool, otherwise use the union.
			if !conditionsEvaluated {
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
		result.Deferred = unknownConditions
		result.NeedsReroute = unknownConditions

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

func hasUsableRouteTier(tiers []operation_setting.RouteTier) bool {
	for _, tier := range tiers {
		if tier.Reject || len(tier.ChannelIDs) > 0 {
			return true
		}
	}
	return false
}

// collectTierChannelIDs returns de-duplicated channel IDs, optionally limited
// to catch-all tiers. Reject tiers never contribute candidates.
func collectTierChannelIDs(tiers []operation_setting.RouteTier, catchAllOnly bool) []int {
	seen := make(map[int]struct{})
	var channelIDs []int
	for _, tier := range tiers {
		if tier.Reject {
			continue
		}
		if catchAllOnly && len(tier.Conditions) > 0 {
			continue
		}
		for _, id := range tier.ChannelIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			channelIDs = append(channelIDs, id)
		}
	}
	return channelIDs
}

func getRouteMetric(c *gin.Context, key constant.ContextKey) (int, bool) {
	if c == nil {
		return 0, false
	}
	value, exists := common.GetContextKey(c, key)
	if !exists || value == nil {
		return 0, false
	}
	return common.GetContextKeyInt(c, key), true
}

func routeTierConditionsKnown(conditions []operation_setting.RouteTierCondition, tokensKnown bool, docsKnown bool) bool {
	for _, condition := range conditions {
		switch condition.Var {
		case "len", "p":
			if !tokensKnown {
				return false
			}
		case "docs":
			if !docsKnown {
				return false
			}
		case "c":
			// Completion tokens retain the existing zero-at-routing-time
			// semantics, but only after at least one request metric is known.
			if !tokensKnown && !docsKnown {
				return false
			}
		}
	}
	return true
}

func evaluateRouteTier(conditions []operation_setting.RouteTierCondition, estimatedTokens int, estimatedDocs int) bool {
	if len(conditions) == 0 {
		return true
	}
	for _, cond := range conditions {
		if !evaluateRouteCondition(cond, estimatedTokens, estimatedDocs) {
			return false
		}
	}
	return true
}

func evaluateRouteCondition(cond operation_setting.RouteTierCondition, estimatedTokens int, estimatedDocs int) bool {
	var actual int
	switch cond.Var {
	case "len", "p":
		actual = estimatedTokens
	case "c":
		actual = 0 // completion tokens unknown at routing time
	case "docs":
		actual = estimatedDocs
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
	// auto/多分组令牌没有单一分组可供规则匹配，回退到用户分组维度
	if usingGroup != "" && !IsMultiCandidateGroup(usingGroup) {
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

	// 多候选模式：auto（用户可用全部分组按全局优先级）或令牌多分组（按令牌内顺序）
	if candidateGroups := ResolveCandidateGroups(param.Ctx, param.TokenGroup); candidateGroups != nil {
		if len(candidateGroups) == 0 {
			return nil, selectGroup, false, errors.New("当前令牌没有可用的分组")
		}

		autoGroups := candidateGroups
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
		if model.IsChannelDailyLimitReached(channelID, channelModel.GetDailyLimitConfig()) {
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

func channelRouteMatchPathRegex(patterns []string, path string) bool {
	if channelRouteMatchAnyRegex(patterns, path) {
		return true
	}
	trimmedPath := strings.TrimRight(path, "/")
	if trimmedPath != path && channelRouteMatchAnyRegex(patterns, trimmedPath) {
		return true
	}
	return trimmedPath != "" && trimmedPath == path && channelRouteMatchAnyRegex(patterns, trimmedPath+"/")
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
		if _, known := getRouteMetric(c, constant.ContextKeyEstimatedTokens); known {
			logInfo["estimated_tokens"] = estimatedTokens
		}
		if estimatedDocs, known := getRouteMetric(c, constant.ContextKeyEstimatedDocs); known {
			logInfo["estimated_docs"] = estimatedDocs
		}
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
		if _, known := getRouteMetric(c, constant.ContextKeyEstimatedTokens); known {
			logInfo["estimated_tokens"] = estimatedTokens
		}
		if estimatedDocs, known := getRouteMetric(c, constant.ContextKeyEstimatedDocs); known {
			logInfo["estimated_docs"] = estimatedDocs
		}
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
