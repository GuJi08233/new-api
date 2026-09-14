package dto

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestClaudeRequestThinkingSummary(t *testing.T) {
	tests := []struct {
		name string
		req  ClaudeRequest
		want string
	}{
		{name: "nothing sent", req: ClaudeRequest{}, want: ""},
		{
			name: "output_config effort wins over thinking",
			req: ClaudeRequest{
				OutputConfig: json.RawMessage(`{"effort":"high"}`),
				Thinking:     &Thinking{Type: "adaptive"},
			},
			want: "high",
		},
		{
			name: "enabled with budget",
			req:  ClaudeRequest{Thinking: &Thinking{Type: "enabled", BudgetTokens: common.GetPointer(16000)}},
			want: "enabled (budget 16000)",
		},
		{
			name: "enabled without budget",
			req:  ClaudeRequest{Thinking: &Thinking{Type: "enabled"}},
			want: "enabled",
		},
		{
			name: "adaptive",
			req:  ClaudeRequest{Thinking: &Thinking{Type: "adaptive"}},
			want: "adaptive",
		},
		{
			name: "disabled",
			req:  ClaudeRequest{Thinking: &Thinking{Type: "disabled"}},
			want: "disabled",
		},
		{
			name: "output_config without effort falls back to thinking",
			req: ClaudeRequest{
				OutputConfig: json.RawMessage(`{"format":{"type":"json_schema"}}`),
				Thinking:     &Thinking{Type: "adaptive"},
			},
			want: "adaptive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.req.ThinkingSummary())
		})
	}
}

func TestGeminiThinkingConfigThinkingSummary(t *testing.T) {
	tests := []struct {
		name string
		cfg  *GeminiThinkingConfig
		want string
	}{
		{name: "nil config", cfg: nil, want: ""},
		{name: "only includeThoughts", cfg: &GeminiThinkingConfig{IncludeThoughts: true}, want: ""},
		{name: "thinkingLevel wins over budget", cfg: &GeminiThinkingConfig{ThinkingLevel: "high", ThinkingBudget: common.GetPointer(1024)}, want: "high"},
		{name: "dynamic budget", cfg: &GeminiThinkingConfig{ThinkingBudget: common.GetPointer(-1)}, want: "dynamic"},
		{name: "zero budget turns thinking off", cfg: &GeminiThinkingConfig{ThinkingBudget: common.GetPointer(0)}, want: "disabled"},
		{name: "explicit budget", cfg: &GeminiThinkingConfig{ThinkingBudget: common.GetPointer(8192)}, want: "budget 8192"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ThinkingSummary())
		})
	}
}
