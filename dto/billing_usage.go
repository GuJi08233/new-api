package dto

import "strings"

const (
	BillingUsageSourceClaudeMessages = "claude_messages"
	BillingUsageSourceGeminiChat     = "gemini_chat"
	BillingUsageSourceOAIChat        = "oai_chat"
	BillingUsageSourceOAIResponses   = "oai_responses"

	BillingUsageSemanticAnthropic = "anthropic"
	BillingUsageSemanticGemini    = "gemini"
	BillingUsageSemanticOpenAI    = "openai"
)

type BillingUsage struct {
	Source              string               `json:"source,omitempty"`
	Semantic            string               `json:"semantic,omitempty"`
	Estimated           bool                 `json:"estimated,omitempty"`
	OpenAIUsage         *Usage               `json:"openai_usage,omitempty"`
	ClaudeUsage         *ClaudeUsage         `json:"claude_usage,omitempty"`
	GeminiUsageMetadata *GeminiUsageMetadata `json:"gemini_usage_metadata,omitempty"`
}

func NewClaudeMessagesBillingUsage(usage *ClaudeUsage) *BillingUsage {
	if !HasClaudeUsageTokens(usage) {
		return nil
	}
	return &BillingUsage{
		Source:      BillingUsageSourceClaudeMessages,
		Semantic:    BillingUsageSemanticAnthropic,
		ClaudeUsage: cloneClaudeUsage(usage),
	}
}

// HasClaudeUsageTokens mirrors HasOpenAIUsageTokens/HasGeminiUsageMetadataTokens:
// an all-zero ClaudeUsage must not become a BillingUsage, otherwise it would take
// precedence during settlement and zero out a non-zero top-level usage.
func HasClaudeUsageTokens(usage *ClaudeUsage) bool {
	if usage == nil {
		return false
	}
	if usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.CacheCreationInputTokens != 0 ||
		usage.CacheReadInputTokens != 0 ||
		usage.ClaudeCacheCreation5mTokens != 0 ||
		usage.ClaudeCacheCreation1hTokens != 0 {
		return true
	}
	if usage.CacheCreation != nil &&
		(usage.CacheCreation.Ephemeral5mInputTokens != 0 || usage.CacheCreation.Ephemeral1hInputTokens != 0) {
		return true
	}
	return false
}

func NewOpenAIChatBillingUsage(usage *Usage) *BillingUsage {
	return newOpenAIBillingUsage(BillingUsageSourceOAIChat, usage)
}

func NewOpenAIResponsesBillingUsage(usage *Usage) *BillingUsage {
	return newOpenAIBillingUsage(BillingUsageSourceOAIResponses, usage)
}

func newOpenAIBillingUsage(source string, usage *Usage) *BillingUsage {
	if !HasOpenAIUsageTokens(usage) {
		return nil
	}
	return &BillingUsage{
		Source:      source,
		Semantic:    BillingUsageSemanticOpenAI,
		OpenAIUsage: cloneOpenAIUsage(usage),
	}
}

func HasOpenAIUsageTokens(usage *Usage) bool {
	if usage == nil {
		return false
	}
	if usage.PromptTokens != 0 ||
		usage.CompletionTokens != 0 ||
		usage.TotalTokens != 0 ||
		usage.InputTokens != 0 ||
		usage.OutputTokens != 0 ||
		usage.PromptCacheHitTokens != 0 ||
		usage.ClaudeCacheCreation5mTokens != 0 ||
		usage.ClaudeCacheCreation1hTokens != 0 {
		return true
	}
	if usage.PromptTokensDetails.CachedTokens != 0 ||
		usage.PromptTokensDetails.CachedCreationTokens != 0 ||
		usage.PromptTokensDetails.CacheWriteTokens != 0 ||
		usage.PromptTokensDetails.TextTokens != 0 ||
		usage.PromptTokensDetails.ImageTokens != 0 ||
		usage.PromptTokensDetails.AudioTokens != 0 {
		return true
	}
	if usage.CompletionTokenDetails.ReasoningTokens != 0 ||
		usage.CompletionTokenDetails.TextTokens != 0 ||
		usage.CompletionTokenDetails.ImageTokens != 0 ||
		usage.CompletionTokenDetails.AudioTokens != 0 {
		return true
	}
	return usage.InputTokensDetails != nil
}

func NewGeminiChatBillingUsage(metadata *GeminiUsageMetadata) *BillingUsage {
	return newGeminiChatBillingUsage(metadata, false)
}

func NewEstimatedGeminiChatBillingUsage(usage *Usage) *BillingUsage {
	if usage == nil {
		return nil
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return newGeminiChatBillingUsage(&GeminiUsageMetadata{
		PromptTokenCount:     usage.PromptTokens,
		CandidatesTokenCount: usage.CompletionTokens,
		TotalTokenCount:      totalTokens,
	}, true)
}

func newGeminiChatBillingUsage(metadata *GeminiUsageMetadata, estimated bool) *BillingUsage {
	if !HasGeminiUsageMetadataTokens(metadata) {
		return nil
	}
	usageMetadata := cloneGeminiUsageMetadata(*metadata)
	return &BillingUsage{
		Source:              BillingUsageSourceGeminiChat,
		Semantic:            BillingUsageSemanticGemini,
		Estimated:           estimated,
		GeminiUsageMetadata: &usageMetadata,
	}
}

func CloneBillingUsage(usage *BillingUsage) *BillingUsage {
	if usage == nil {
		return nil
	}
	clone := *usage
	clone.OpenAIUsage = cloneOpenAIUsage(usage.OpenAIUsage)
	clone.ClaudeUsage = cloneClaudeUsage(usage.ClaudeUsage)
	if usage.GeminiUsageMetadata != nil {
		metadata := cloneGeminiUsageMetadata(*usage.GeminiUsageMetadata)
		clone.GeminiUsageMetadata = &metadata
	}
	return &clone
}

func cloneOpenAIUsage(usage *Usage) *Usage {
	if usage == nil {
		return nil
	}
	clone := *usage
	clone.BillingUsage = nil
	if usage.InputTokensDetails != nil {
		inputTokensDetails := *usage.InputTokensDetails
		clone.InputTokensDetails = &inputTokensDetails
	}
	return &clone
}

func cloneClaudeUsage(usage *ClaudeUsage) *ClaudeUsage {
	if usage == nil {
		return nil
	}
	clone := *usage
	clone.BillingUsage = nil
	if usage.CacheCreation != nil {
		cacheCreation := *usage.CacheCreation
		clone.CacheCreation = &cacheCreation
		clone.ClaudeCacheCreation5mTokens = cacheCreation.Ephemeral5mInputTokens
		clone.ClaudeCacheCreation1hTokens = cacheCreation.Ephemeral1hInputTokens
	}
	if usage.ServerToolUse != nil {
		serverToolUse := *usage.ServerToolUse
		clone.ServerToolUse = &serverToolUse
	}
	return &clone
}

// MergeInputTokenDetails 按字段保留最后一个非零快照，不让缺失项擦掉前帧统计。
func MergeInputTokenDetails(previous, incoming InputTokenDetails) InputTokenDetails {
	if incoming.CachedTokens != 0 {
		previous.CachedTokens = incoming.CachedTokens
	}
	if incoming.CachedCreationTokens != 0 {
		previous.CachedCreationTokens = incoming.CachedCreationTokens
	}
	if incoming.CacheWriteTokens != 0 {
		previous.CacheWriteTokens = incoming.CacheWriteTokens
	}
	if incoming.TextTokens != 0 {
		previous.TextTokens = incoming.TextTokens
	}
	if incoming.AudioTokens != 0 {
		previous.AudioTokens = incoming.AudioTokens
	}
	if incoming.ImageTokens != 0 {
		previous.ImageTokens = incoming.ImageTokens
	}
	return previous
}

// MergeGeminiUsageMetadataNonZero 合并累计快照，分模态重复条目先求和再覆盖。
func MergeGeminiUsageMetadataNonZero(previous, incoming *GeminiUsageMetadata) *GeminiUsageMetadata {
	if incoming == nil {
		if previous == nil {
			return nil
		}
		clone := cloneGeminiUsageMetadata(*previous)
		return &clone
	}
	var merged GeminiUsageMetadata
	if previous != nil {
		merged = cloneGeminiUsageMetadata(*previous)
	}
	if incoming.PromptTokenCount != 0 {
		merged.PromptTokenCount = incoming.PromptTokenCount
	}
	if incoming.ToolUsePromptTokenCount != 0 {
		merged.ToolUsePromptTokenCount = incoming.ToolUsePromptTokenCount
	}
	if incoming.CandidatesTokenCount != 0 {
		merged.CandidatesTokenCount = incoming.CandidatesTokenCount
	}
	if incoming.TotalTokenCount != 0 {
		merged.TotalTokenCount = incoming.TotalTokenCount
	}
	if incoming.ThoughtsTokenCount != 0 {
		merged.ThoughtsTokenCount = incoming.ThoughtsTokenCount
	}
	if incoming.CachedContentTokenCount != 0 {
		merged.CachedContentTokenCount = incoming.CachedContentTokenCount
	}
	merged.PromptTokensDetails = mergeGeminiModalityDetails(merged.PromptTokensDetails, incoming.PromptTokensDetails)
	merged.ToolUsePromptTokensDetails = mergeGeminiModalityDetails(merged.ToolUsePromptTokensDetails, incoming.ToolUsePromptTokensDetails)
	merged.CandidatesTokensDetails = mergeGeminiModalityDetails(merged.CandidatesTokensDetails, incoming.CandidatesTokensDetails)
	if incoming.BillingUsage != nil {
		merged.BillingUsage = CloneBillingUsage(incoming.BillingUsage)
		if previous != nil && previous.BillingUsage != nil {
			merged.BillingUsage.Estimated = merged.BillingUsage.Estimated || previous.BillingUsage.Estimated
		}
	} else if previous != nil {
		merged.BillingUsage = CloneBillingUsage(previous.BillingUsage)
	}
	return &merged
}

func mergeGeminiModalityDetails(previous, incoming []GeminiPromptTokensDetails) []GeminiPromptTokensDetails {
	counts := make(map[string]int)
	order := make([]string, 0)
	for _, snapshot := range [][]GeminiPromptTokensDetails{previous, incoming} {
		current := make(map[string]int)
		for _, detail := range snapshot {
			key := strings.ToUpper(strings.TrimSpace(detail.Modality))
			if _, exists := counts[key]; !exists {
				counts[key] = 0
				order = append(order, key)
			}
			current[key] += detail.TokenCount
		}
		for key, count := range current {
			if count != 0 {
				counts[key] = count
			}
		}
	}
	result := make([]GeminiPromptTokensDetails, 0, len(order))
	for _, key := range order {
		result = append(result, GeminiPromptTokensDetails{Modality: key, TokenCount: counts[key]})
	}
	return result
}

func cloneGeminiUsageMetadata(metadata GeminiUsageMetadata) GeminiUsageMetadata {
	metadata.PromptTokensDetails = append([]GeminiPromptTokensDetails{}, metadata.PromptTokensDetails...)
	metadata.ToolUsePromptTokensDetails = append([]GeminiPromptTokensDetails{}, metadata.ToolUsePromptTokensDetails...)
	metadata.CandidatesTokensDetails = append([]GeminiPromptTokensDetails{}, metadata.CandidatesTokensDetails...)
	metadata.BillingUsage = nil
	return metadata
}

func HasGeminiUsageMetadataTokens(metadata *GeminiUsageMetadata) bool {
	if metadata == nil {
		return false
	}
	if metadata.PromptTokenCount != 0 ||
		metadata.ToolUsePromptTokenCount != 0 ||
		metadata.CandidatesTokenCount != 0 ||
		metadata.TotalTokenCount != 0 ||
		metadata.ThoughtsTokenCount != 0 ||
		metadata.CachedContentTokenCount != 0 {
		return true
	}
	for _, detail := range metadata.PromptTokensDetails {
		if detail.TokenCount != 0 {
			return true
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.TokenCount != 0 {
			return true
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		if detail.TokenCount != 0 {
			return true
		}
	}
	return false
}
