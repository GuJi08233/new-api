package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestKimiToolLoadingMessagePreservesToolsWithoutContent(t *testing.T) {
	var request GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"kimi-k3","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"lookup"}}]},{"role":"assistant","content":null}]}`, &request))
	data, err := common.Marshal(request)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(data, "messages.0.tools").Exists())
	assert.False(t, gjson.GetBytes(data, "messages.0.content").Exists())
	assert.True(t, gjson.GetBytes(data, "messages.1.content").Exists())
	assert.Contains(t, request.GetTokenCountMeta().CombineText, "lookup")
}

func TestChatResponseChoiceKeepsProtocolEnvelope(t *testing.T) {
	data, err := common.Marshal(OpenAITextResponse{Choices: []OpenAITextResponseChoice{{Index: 2, FinishReason: "tool_calls", Message: Message{Role: "assistant", Content: "answer"}}}})
	require.NoError(t, err)
	assert.Equal(t, int64(2), gjson.GetBytes(data, "choices.0.index").Int())
	assert.Equal(t, "tool_calls", gjson.GetBytes(data, "choices.0.finish_reason").String())
	assert.Equal(t, "assistant", gjson.GetBytes(data, "choices.0.message.role").String())
	assert.Equal(t, "answer", gjson.GetBytes(data, "choices.0.message.content").String())
}
