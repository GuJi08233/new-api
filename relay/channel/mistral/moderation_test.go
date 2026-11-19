package mistral

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func moderationTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/moderations", nil)
	return c, w
}

func moderationTestInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeModerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMistral,
			ChannelBaseUrl:    "https://api.mistral.ai",
			UpstreamModelName: "mistral-moderation-latest",
		},
		OriginModelName: "mistral-moderation-latest",
	}
}

func TestMistralModerationRequestAndURL(t *testing.T) {
	for _, input := range []any{"hello", []string{"hello", "world"}, []any{"hello", "world"}} {
		req := &dto.GeneralOpenAIRequest{
			Model:           "mistral-moderation-latest",
			Input:           input,
			Stream:          common.GetPointer(false),
			Temperature:     common.GetPointer(0.0),
			ReasoningEffort: "high",
			Messages:        []dto.Message{{Role: "user", Content: "ignore"}},
			Metadata:        []byte(`{"zero":0,"enabled":false}`),
		}
		converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, moderationTestInfo(), req)
		require.NoError(t, err)
		raw, err := common.Marshal(converted)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, common.Unmarshal(raw, &body))
		assert.Len(t, body, 3)
		assert.Equal(t, "mistral-moderation-latest", body["model"])
		assert.Equal(t, false, body["metadata"].(map[string]any)["enabled"])
		assert.NotContains(t, body, "messages")
		assert.NotContains(t, body, "temperature")
	}

	for _, badInput := range []any{
		nil, "", []string{}, []any{}, []any{"hello", ""}, []any{"hello", 12},
		[]any{map[string]any{"type": "image_url", "image_url": "https://example.com/image.png"}},
	} {
		_, err := (&Adaptor{}).convertModerationRequest(&dto.GeneralOpenAIRequest{Input: badInput})
		require.Error(t, err)
		apiErr, ok := err.(*types.NewAPIError)
		require.True(t, ok)
		assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	}

	_, err := (&Adaptor{}).convertModerationRequest(&dto.GeneralOpenAIRequest{Input: "hi", Stream: common.GetPointer(true)})
	require.ErrorContains(t, err, "streaming")

	for _, base := range []string{"https://api.mistral.ai", "https://api.mistral.ai/v1/"} {
		info := moderationTestInfo()
		info.ChannelBaseUrl = base
		u, err := (&Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, "https://api.mistral.ai/v1/moderations", u)
	}

	assert.True(t, IsModerationModel("mistral-moderation-latest"))
	assert.True(t, IsModerationModel("Mistral-Moderation-2603"))
	assert.False(t, IsModerationModel("mistral-large-latest"))
	assert.Equal(t, []constant.EndpointType{constant.EndpointTypeModerations}, common.GetEndpointTypesByChannelType(constant.ChannelTypeMistral, "mistral-moderation-latest"))
}

func TestMistralModerationResponseAndFallbackUsage(t *testing.T) {
	body := `{"id":"mod-1","model":"mistral-moderation-2603","future":false,"results":[{"categories":{"violence_and_threats":false,"financial":false},"category_scores":{"violence_and_threats":0,"financial":0.01}},{"categories":{"violence_and_threats":true,"financial":false},"category_scores":{"violence_and_threats":0.99,"financial":0}}],"usage":{"prompt_tokens":81,"completion_tokens":0,"total_tokens":142}}`
	c, w := moderationTestContext()
	info := moderationTestInfo()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	usageAny, apiErr := (&Adaptor{moderationInputs: 2}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	usage := usageAny.(*dto.Usage)
	assert.Equal(t, 81, usage.PromptTokens)
	assert.Zero(t, usage.CompletionTokens)
	assert.Equal(t, 142, usage.TotalTokens)

	var response map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	results := response["results"].([]any)
	assert.Equal(t, false, results[0].(map[string]any)["flagged"])
	assert.Equal(t, true, results[1].(map[string]any)["flagged"])

	// When upstream omits usage, fall back to estimated input tokens without fabricating usage in response body.
	c, w = moderationTestContext()
	info = moderationTestInfo()
	info.SetEstimatePromptTokens(9)
	resp2 := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"mod-2","model":"old-model","results":[{"categories":{"health":true},"category_scores":{"health":0.8}}]}`)),
	}
	usageAny, apiErr = (&Adaptor{}).DoResponse(c, resp2, info)
	require.Nil(t, apiErr)
	assert.Equal(t, 9, usageAny.(*dto.Usage).PromptTokens)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
	assert.NotContains(t, w.Body.String(), "usage")
}

func TestMistralModerationRejectsMalformedResponseAndBadFinalBody(t *testing.T) {
	for _, body := range []string{
		`null`, `{}`, `{"id":"m","model":"m","results":[]}`,
		`{"id":"m","model":"m","results":[{"category_scores":{"health":0.1}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":null},"category_scores":{"health":0.1}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":null}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":0.1}}],"usage":"invalid"}`,
		`{"object":"error","message":"bad","type":"invalid_request","code":"3051"}`,
	} {
		c, w := moderationTestContext()
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}
		_, apiErr := (&Adaptor{moderationInputs: 1}).DoResponse(c, resp, moderationTestInfo())
		require.NotNil(t, apiErr, body)
		assert.Empty(t, w.Body.String())
	}

	c, w := moderationTestContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":0.1}}]}`)),
	}
	_, apiErr := (&Adaptor{moderationInputs: 2}).DoResponse(c, resp, moderationTestInfo())
	require.NotNil(t, apiErr)
	assert.Empty(t, w.Body.String())

	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/moderations", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	c, _ = moderationTestContext()
	info := moderationTestInfo()
	info.ChannelBaseUrl = upstream.URL
	a := &Adaptor{moderationInputs: 1}
	_, err := a.DoRequest(c, info, strings.NewReader(`{"model":"mistral-moderation-latest","input":["a","b"]}`))
	require.NoError(t, err)
	assert.Equal(t, 2, a.moderationInputs)

	_, err = a.DoRequest(c, info, strings.NewReader(`{"model":"m","input":"hi","stream":true}`))
	require.ErrorContains(t, err, "streaming")
}
