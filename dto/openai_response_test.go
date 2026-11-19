package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamResponseAcceptsFloatCreatedAt(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "float", body: `{"type":"response.completed","response":{"id":"resp_1","created_at":1795069013.0,"model":"m","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`, want: 1795069013},
		{name: "integer", body: `{"type":"response.completed","response":{"id":"resp_1","created_at":1795069013,"model":"m"}}`, want: 1795069013},
		{name: "null", body: `{"type":"response.completed","response":{"id":"resp_1","created_at":null,"model":"m"}}`, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event ResponsesStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(tt.body, &event))
			require.NotNil(t, event.Response)
			assert.Equal(t, tt.want, event.Response.CreatedAt)
			assert.Equal(t, "resp_1", event.Response.ID)
			assert.Equal(t, "m", event.Response.Model)
		})
	}

	var event ResponsesStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(tests[0].body, &event))
	require.NotNil(t, event.Response.Usage)
	assert.Equal(t, 5, event.Response.Usage.TotalTokens)
}
