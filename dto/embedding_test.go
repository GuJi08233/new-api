package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddingRequestGetInputCount(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{name: "single string", input: "hello", want: 1},
		{name: "string batch", input: []any{"a", "b", "c"}, want: 3},
		{name: "typed string batch", input: []string{"a", "b"}, want: 2},
		{name: "flat token input", input: []any{1, 2, 3}, want: 1},
		{name: "typed flat token input", input: []int{1, 2, 3}, want: 1},
		{name: "token batch", input: []any{[]any{1, 2}, []any{3, 4}}, want: 2},
		{name: "empty batch", input: []any{}, want: 0},
		{name: "nil", input: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &EmbeddingRequest{Input: tt.input}
			assert.Equal(t, tt.want, request.GetInputCount())
		})
	}
}

func TestEmbeddingRequestParseInputSupportsTypedStringSlice(t *testing.T) {
	request := &EmbeddingRequest{Input: []string{"a", "b"}}
	assert.Equal(t, []string{"a", "b"}, request.ParseInput())
}

func TestGetRequestDocumentCount(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		want    int
		known   bool
	}{
		{name: "rerank", request: &RerankRequest{Documents: []any{"a", "b"}}, want: 2, known: true},
		{name: "embedding", request: &EmbeddingRequest{Input: []any{"a", "b", "c"}}, want: 3, known: true},
		{name: "gemini single", request: &GeminiEmbeddingRequest{}, want: 1, known: true},
		{name: "gemini batch", request: &GeminiBatchEmbeddingRequest{Requests: []*GeminiEmbeddingRequest{{}, {}}}, want: 2, known: true},
		{name: "chat has no docs metric", request: &BaseRequest{}, want: 0, known: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, known := GetRequestDocumentCount(tt.request)
			assert.Equal(t, tt.want, count)
			assert.Equal(t, tt.known, known)
		})
	}
}
