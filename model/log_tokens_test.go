package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogTokenCounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt int
		other  string
		input  int
	}{
		{"plain", 100, "", 100},
		{"openai cache included", 1000, `{"cache_tokens":900,"cache_creation_tokens":50}`, 1000},
		{"anthropic cache read", 100, `{"usage_semantic":"anthropic","cache_tokens":900}`, 1000},
		{"legacy claude", 100, `{"claude":true,"cache_tokens":900}`, 1000},
		{"cache only", 0, `{"claude":true,"cache_tokens":900}`, 900},
		{"aggregate includes TTL remainder", 100, `{"claude":true,"cache_creation_tokens":80,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 180},
		{"TTL split exceeds aggregate", 100, `{"claude":true,"cache_creation_tokens":20,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 160},
		{"normalized write aliases", 100, `{"claude":true,"cache_write_tokens":80,"cache_creation_tokens":80,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 180},
		{"legacy conversion", 62, `{"cache_tokens":3544,"cache_creation_tokens_5m":586}`, 4192},
		{"tagged conversion includes cache", 4192, `{"usage_source":"anthropic","cache_tokens":3544,"cache_creation_tokens_5m":586}`, 4192},
		{"explicit total overrides semantic", 100, `{"input_tokens_total":1000,"usage_semantic":"anthropic","cache_tokens":900}`, 1000},
		{"explicit zero total", 100, `{"input_tokens_total":0,"claude":true,"cache_tokens":900}`, 0},
		{"malformed metadata", 100, `{"claude":true,`, 100},
		{"negative counts", -5, `{"claude":true,"cache_tokens":-1,"cache_creation_tokens":-2}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, output := logTokenCounts(tc.prompt, 50, tc.other)
			assert.Equal(t, tc.input, input)
			assert.Equal(t, 50, output)
		})
	}
	_, output := logTokenCounts(100, -1, "")
	assert.Zero(t, output)
}

func TestLogsFilterByIPAndSumUsedQuotaBreakdown(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{UserId: 1, Username: "alice", CreatedAt: now - 5, Type: LogTypeConsume, Quota: 10, PromptTokens: 100, CompletionTokens: 50, Ip: "203.0.113.10", Other: `{"claude":true,"cache_tokens":900}`},
		{UserId: 1, Username: "alice", CreatedAt: now - 5, Type: LogTypeError, Quota: 0, PromptTokens: 999, CompletionTokens: 999, Ip: "203.0.113.10"},
		{UserId: 2, Username: "bob", CreatedAt: now - 5, Type: LogTypeConsume, Quota: 20, PromptTokens: 200, CompletionTokens: 40, Ip: "203.0.113.20"},
		{UserId: 1, Username: "alice", CreatedAt: now - 120, Type: LogTypeConsume, Quota: 5, PromptTokens: 500, CompletionTokens: 500, Ip: "203.0.113.10"},
	}).Error)

	allLogs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "", "203.0.113.10")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, allLogs, 3)

	userLogs, userTotal, err := GetUserLogs(1, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "", "203.0.113.10")
	require.NoError(t, err)
	assert.EqualValues(t, 2, userTotal)
	require.Len(t, userLogs, 2)

	stat, err := SumUsedQuota(LogTypeConsume, now-150, now+10, "", "alice", "", 0, "", "203.0.113.10")
	require.NoError(t, err)
	assert.Equal(t, 15, stat.Quota)
	assert.Equal(t, 1, stat.Rpm)
	assert.Equal(t, 2, stat.RpmTotal)
	assert.Equal(t, float64(50), stat.RpmSuccessRate)
	assert.Equal(t, 1000, stat.InputTpm)
	assert.Equal(t, 50, stat.OutputTpm)
	assert.Equal(t, 1050, stat.Tpm)
}
