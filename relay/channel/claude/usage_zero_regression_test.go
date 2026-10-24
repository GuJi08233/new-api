package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeZeroUsageAndEstimatedOutputBillingSnapshot(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name         string
		text         bool
		done         bool
		wantEstimate bool
	}{
		{name: "empty completed refusal preserves zero", done: true},
		{name: "completed response with text estimates zero output", text: true, done: true, wantEstimate: true},
		{name: "interrupted response estimates delivered text", text: true, wantEstimate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			info.SetEstimatePromptTokens(999)
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			frames := []string{`{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`}
			if tc.text {
				frames = append(frames, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello world"}}`)
			}
			if tc.done {
				frames = append(frames, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`)
			}
			for _, frame := range frames {
				var response dto.ClaudeResponse
				require.NoError(t, common.UnmarshalJsonStr(frame, &response))
				FormatClaudeResponseInfo(&response, nil, state)
			}
			FinalizeStreamUsage(c, info, state)
			assert.Zero(t, state.Usage.PromptTokens, "explicit input zero must not use the request estimate")
			assert.Equal(t, 100, state.Usage.PromptTokensDetails.CachedTokens)
			require.NotNil(t, state.Usage.BillingUsage)
			billing := state.Usage.BillingUsage
			require.NotNil(t, billing.ClaudeUsage)
			assert.Zero(t, billing.ClaudeUsage.InputTokens)
			assert.Equal(t, 100, billing.ClaudeUsage.CacheReadInputTokens)
			assert.Equal(t, 20, billing.ClaudeUsage.CacheCreationInputTokens)
			assert.Equal(t, state.Usage.CompletionTokens, billing.ClaudeUsage.OutputTokens, "settlement must receive locally recovered output")
			assert.Equal(t, tc.wantEstimate, billing.Estimated)
			if tc.wantEstimate {
				assert.Positive(t, state.Usage.CompletionTokens)
			} else {
				assert.Zero(t, state.Usage.CompletionTokens)
			}
			require.NotNil(t, state.RawUsage)
			assert.Zero(t, state.RawUsage.OutputTokens, "local estimation must not mutate the original upstream usage")
		})
	}
}

func TestClaudeTerminalExplicitZeroReplacesEarlierCounters(t *testing.T) {
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	for _, frame := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":5,"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":2}}}}`,
		`{"type":"message_delta","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`,
	} {
		var response dto.ClaudeResponse
		require.NoError(t, common.UnmarshalJsonStr(frame, &response))
		FormatClaudeResponseInfo(&response, nil, state)
	}
	assert.Zero(t, state.Usage.PromptTokens)
	assert.Zero(t, state.Usage.CompletionTokens)
	assert.Zero(t, state.Usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, state.Usage.BillingUsage)
	assert.Zero(t, state.Usage.BillingUsage.ClaudeUsage.InputTokens)
	assert.Zero(t, state.Usage.BillingUsage.ClaudeUsage.OutputTokens)
	assert.Zero(t, state.Usage.ClaudeCacheCreation5mTokens)
	assert.Zero(t, state.Usage.ClaudeCacheCreation1hTokens)
	assert.Nil(t, state.Usage.BillingUsage.ClaudeUsage.CacheCreation)
}

func TestClaudeFallbackForToolOutputAndMissingInput(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name       string
		startUsage string
		tool       bool
		done       bool
		wantPrompt int
	}{
		{name: "completed tool output", startUsage: `{"input_tokens":0,"output_tokens":0}`, tool: true, done: true},
		{name: "interrupted tool output", startUsage: `{"input_tokens":0,"output_tokens":0}`, tool: true},
		{name: "missing ordinary input is estimated", startUsage: `{"output_tokens":0}`, done: true, wantPrompt: 999},
		{name: "omitted input with reported cache is not estimated twice", startUsage: `{"output_tokens":0,"cache_read_input_tokens":100}`, done: true},
		{name: "omitted input with reported cache during interruption", startUsage: `{"output_tokens":0,"cache_read_input_tokens":100}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			info.SetEstimatePromptTokens(999)
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			frames := []string{`{"type":"message_start","message":{"usage":` + tc.startUsage + `}}`}
			if tc.tool {
				frames = append(frames, `{"type":"content_block_start","content_block":{"type":"tool_use","name":"lookup","input":{}}}`, `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"query\":\"hello\"}"}}`)
			} else {
				frames = append(frames, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello world"}}`)
			}
			if tc.done {
				frames = append(frames, `{"type":"message_delta","usage":{"output_tokens":0}}`)
			}
			for _, frame := range frames {
				var response dto.ClaudeResponse
				require.NoError(t, common.UnmarshalJsonStr(frame, &response))
				FormatClaudeResponseInfo(&response, nil, state)
			}
			FinalizeStreamUsage(c, info, state)
			assert.Equal(t, tc.wantPrompt, state.Usage.PromptTokens)
			assert.Positive(t, state.Usage.CompletionTokens)
			require.NotNil(t, state.Usage.BillingUsage)
			assert.Equal(t, tc.wantPrompt, state.Usage.BillingUsage.ClaudeUsage.InputTokens)
			assert.Equal(t, state.Usage.CompletionTokens, state.Usage.BillingUsage.ClaudeUsage.OutputTokens)
			assert.True(t, state.Usage.BillingUsage.Estimated)
			assert.Zero(t, state.RawUsage.OutputTokens)
		})
	}
}
