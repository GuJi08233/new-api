package common

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	common2 "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPassThroughTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	return c
}

func TestGetPassThroughRequestBodyRewritesMappedModel(t *testing.T) {
	c := newPassThroughTestContext(t, `{"model":"gpt-4o","stream":true}`)
	info := &RelayInfo{
		ChannelMeta: &ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{PassThroughRewriteModelEnabled: true},
			IsModelMapped:     true,
			UpstreamModelName: "gpt-4o-upstream",
		},
	}

	reader, err := GetPassThroughRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.JSONEq(t, `{"model":"gpt-4o-upstream","stream":true}`, string(data))
	assert.Equal(t, int64(len(data)), info.UpstreamRequestBodySize)
}

func TestGetPassThroughRequestBodyKeepsRawBodyWhenRewriteDisabled(t *testing.T) {
	raw := `{"model":"gpt-4o","stream":true}`
	c := newPassThroughTestContext(t, raw)
	info := &RelayInfo{
		ChannelMeta: &ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{},
			IsModelMapped:     true,
			UpstreamModelName: "gpt-4o-upstream",
		},
	}

	reader, err := GetPassThroughRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, raw, string(data))
	assert.Equal(t, int64(len(raw)), info.UpstreamRequestBodySize)
}

func TestGetPassThroughRequestBodyIgnoresBodyWithoutModelField(t *testing.T) {
	raw := `{"contents":[{"parts":[{"text":"hi"}]}]}`
	c := newPassThroughTestContext(t, raw)
	info := &RelayInfo{
		ChannelMeta: &ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{PassThroughRewriteModelEnabled: true},
			IsModelMapped:     true,
			UpstreamModelName: "gemini-2.5-pro",
		},
	}

	reader, err := GetPassThroughRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, raw, string(data))
}

func TestDecisionsPassThroughMapsGatewayAliasAndPreservesNativeFields(t *testing.T) {
	raw := `{"model":"gpt-6-luna-decisions","input":"hello","questions":[{"type":"choice","instructions":"Which?","choices":[{"value":false},{"value":true}]}],"future_option":0}`
	c := newPassThroughTestContext(t, raw)
	info := &RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIDecisions,
		OriginModelName: "gpt-6-luna-decisions",
		ChannelMeta:     &ChannelMeta{IsModelMapped: true, UpstreamModelName: "gpt-6-luna"},
	}
	reader, err := GetPassThroughRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, strings.Replace(raw, "gpt-6-luna-decisions", "gpt-6-luna", 1), string(data))
}

func TestChannelBodyPassthroughPreservesDecisionsAliasUnlessRewriteEnabled(t *testing.T) {
	raw := "{\n \"model\":\"gpt-6-luna-decisions\", \"future_option\":false\n}"
	for _, rewrite := range []bool{false, true} {
		c := newPassThroughTestContext(t, raw)
		t.Cleanup(func() { common2.CleanupBodyStorage(c) })
		info := &RelayInfo{
			RelayFormat: types.RelayFormatOpenAIDecisions, OriginModelName: "gpt-6-luna-decisions",
			ChannelMeta: &ChannelMeta{
				IsModelMapped: true, UpstreamModelName: "gpt-6-luna",
				ChannelSetting: dto.ChannelSettings{
					PassThroughBodyEnabled: true, PassThroughRewriteModelEnabled: rewrite,
				},
			},
		}
		reader, err := GetPassThroughRequestBody(c, info)
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		expected := raw
		if rewrite {
			expected = strings.Replace(raw, "gpt-6-luna-decisions", "gpt-6-luna", 1)
		}
		assert.Equal(t, expected, string(data))
	}
}
