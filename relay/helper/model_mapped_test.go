package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newModelMappingContext(t *testing.T, modelMapping string, originalModel string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", modelMapping)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, originalModel)
	return c
}

// TestModelMappedHelperIdentityMappingClearsPreviousRewrite guards the retry path
// where the previous channel redirected and the next channel maps the model to
// itself: the stale rewrite record must go, and the request must be sent under
// the name the client asked for rather than the previous channel's target.
func TestModelMappedHelperIdentityMappingClearsPreviousRewrite(t *testing.T) {
	c := newModelMappingContext(t, `{"gpt-4":"gpt-4"}`, "gpt-4")
	// Previous attempt ran on a channel that mapped gpt-4 -> gpt-4o.
	service.SetModelRewrite(c, nil, "gpt-4o", "gpt-4")
	request := &dto.GeneralOpenAIRequest{Model: "gpt-4o"}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4"}}
	info.OriginModelName = "gpt-4"

	require.NoError(t, ModelMappedHelper(c, info, request))

	assert.False(t, info.IsModelMapped)
	assert.Equal(t, "gpt-4", info.UpstreamModelName)
	assert.Equal(t, "gpt-4", request.Model)
	_, ok := service.GetModelRewrite(c)
	assert.False(t, ok)
}

// TestModelMappedHelperRecordsClientModelForResponsesCompact pins that the rewrite
// target is the model the client sent: on /v1/responses/compact the distributor
// stores the name with the gateway-internal compact suffix, which must not leak
// back into response bodies or masked error text.
func TestModelMappedHelperRecordsClientModelForResponsesCompact(t *testing.T) {
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5")
	c := newModelMappingContext(t, `{"gpt-5":"gpt-5-max"}`, compactModel)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: compactModel}}
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	info.OriginModelName = compactModel

	require.NoError(t, ModelMappedHelper(c, info, nil))

	require.True(t, info.IsModelMapped)
	assert.Equal(t, "gpt-5-max", info.UpstreamModelName)
	rewrite, ok := service.GetModelRewrite(c)
	require.True(t, ok)
	assert.Equal(t, "gpt-5-max", rewrite.Upstream)
	assert.Equal(t, "gpt-5", rewrite.Request)
}
