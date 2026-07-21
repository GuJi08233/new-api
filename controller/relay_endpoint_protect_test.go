package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsEndpointAllowedGeminiProtectedRoute(t *testing.T) {
	configuredEndpoints := parseEndpoints(`{
		"gemini": {
			"path": "/v1beta/models/{model}:generateContent",
			"method": "POST"
		}
	}`)
	require.Equal(t, []string{"/v1beta/models/{model}:generateContent"}, configuredEndpoints)

	tests := []struct {
		name        string
		requestPath string
		want        bool
	}{
		{
			name:        "v1 streaming route",
			requestPath: "/v1/models/gemini-3.5-flash-lite:streamGenerateContent",
			want:        true,
		},
		{
			name:        "v1beta non-streaming route",
			requestPath: "/v1beta/models/gemini-3.5-flash-lite:generateContent",
			want:        true,
		},
		{
			name:        "different Gemini action",
			requestPath: "/v1beta/models/gemini-3.5-flash-lite:countTokens",
			want:        false,
		},
		{
			name:        "unrelated route",
			requestPath: "/v1/chat/completions",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEndpointAllowed(tt.requestPath, configuredEndpoints))
		})
	}
}
