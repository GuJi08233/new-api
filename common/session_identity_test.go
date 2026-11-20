package common

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeAndHashSessionIdentifier(t *testing.T) {
	assert.Equal(t, "sess-1", NormalizeSessionIdentifier("  sess-1 \t"))
	assert.Empty(t, NormalizeSessionIdentifier(""))
	assert.Empty(t, NormalizeSessionIdentifier("bad\nline"))
	assert.Empty(t, NormalizeSessionIdentifier(strings.Repeat("a", 513)))
	assert.Empty(t, SessionIdentifierHash(""))
	assert.NotEqual(t, SessionIdentifierHash("Abc"), SessionIdentifierHash("abc"))
}

func TestExtractRequestSessionIdentity(t *testing.T) {
	tests := []struct {
		name      string
		headers   http.Header
		body      string
		wantID    string
		wantSrc   string
		wantCache string
		wantCSrc  string
	}{
		{
			name:      "claude code header and prompt_cache_key",
			headers:   http.Header{"X-Claude-Code-Session-Id": {"cc-123"}},
			body:      `{"model":"claude-3-7-sonnet","prompt_cache_key":"cache-abc"}`,
			wantID:    "cc-123",
			wantSrc:   "header:x-claude-code-session-id",
			wantCache: "cache-abc",
			wantCSrc:  "body:prompt_cache_key",
		},
		{
			name:      "opencode treats x-session-id as cache key",
			headers:   http.Header{"User-Agent": {"opencode/1.2"}, "X-Session-Id": {"oc-cache-1"}},
			body:      `{}`,
			wantCache: "oc-cache-1",
			wantCSrc:  "header:x-session-id",
		},
		{
			name:    "claude metadata json embedded session_id",
			headers: http.Header{},
			body:    `{"metadata":{"user_id":"{\"session_id\":\"json-sess-9\"}"}}`,
			wantID:  "json-sess-9",
			wantSrc: "body:metadata.user_id.session_id",
		},
		{
			name:    "claude metadata structured user_account_session",
			headers: http.Header{},
			body:    `{"metadata":{"user_id":"user_42_account_acct1_session_s-7788"}}`,
			wantID:  "s-7788",
			wantSrc: "body:metadata.user_id.session_id",
		},
		{
			name:    "kimi cli plain metadata.user_id",
			headers: http.Header{"User-Agent": {"KimiCLI/0.5"}},
			body:    `{"metadata":{"user_id":"kimi-session-42"}}`,
			wantID:  "kimi-session-42",
			wantSrc: "body:metadata.user_id",
		},
		{
			name:    "generic client plain metadata.user_id ignored",
			headers: http.Header{"User-Agent": {"anthropic-sdk-python/0.40"}},
			body:    `{"metadata":{"user_id":"regular-end-user-id"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractRequestSessionIdentity(tc.headers, []byte(tc.body))
			assert.Equal(t, tc.wantID, got.SessionID)
			assert.Equal(t, tc.wantSrc, got.SessionSource)
			assert.Equal(t, tc.wantCache, got.CacheKey)
			assert.Equal(t, tc.wantCSrc, got.CacheSource)
		})
	}
}
