package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGeminiEmbeddingRequestPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "single embedding", path: "/v1beta/models/embed-model:embedContent", want: true},
		{name: "batch embedding", path: "/v1beta/models/embed-model:batchEmbedContents", want: true},
		{name: "generation", path: "/v1beta/models/gemini-model:generateContent", want: false},
		{name: "token count", path: "/v1beta/models/gemini-model:countTokens", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isGeminiEmbeddingRequestPath(tt.path))
		})
	}
}
