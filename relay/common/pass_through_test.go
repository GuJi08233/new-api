package common

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
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
