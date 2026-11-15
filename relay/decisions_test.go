package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionsAdaptorsSendNativePathAuthenticationAndBody(t *testing.T) {
	service.InitHttpClient()
	for _, tt := range []struct {
		name        string
		format      types.RelayFormat
		channelType int
		path        string
		body        string
		response    string
	}{
		{"gemini", types.RelayFormatGeminiDecisions, constant.ChannelTypeGemini, "/v1beta/decisions", `{"model":"gemini-decision-1","input":"hello","questions":{"greeting":{"type":"noul","instructions":"Greeting?"}}}`, `{"answers":{"greeting":{"type":"noul","noul":0.99}},"usage":{"total_input_tokens":26,"total_output_tokens":0}}`},
		{"openai", types.RelayFormatOpenAIDecisions, constant.ChannelTypeOpenAI, "/v1/decisions", `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"predicate","name":"greeting","instructions":"Greeting?"}]}`, `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"greeting","probability":1}],"usage":{"input_tokens":26,"output_tokens":0}}`},
		{"oa2 gemini", types.RelayFormatGeminiDecisions, constant.ChannelTypeOA2, "/v1beta/decisions", `{"model":"gemini-decision-1","input":"hello","questions":{"greeting":{"type":"noul","instructions":"Greeting?"}}}`, `{"answers":{"greeting":{"type":"noul","noul":0.99}},"usage":{"total_input_tokens":26,"total_output_tokens":0}}`},
		{"oa2 openai", types.RelayFormatOpenAIDecisions, constant.ChannelTypeOA2, "/v1/decisions", `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"predicate","name":"greeting","instructions":"Greeting?"}]}`, `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"greeting","probability":1}],"usage":{"input_tokens":26,"output_tokens":0}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type upstreamRequest struct {
				path, query, body string
				header            http.Header
				err               error
			}
			received := make(chan upstreamRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- upstreamRequest{r.URL.Path, r.URL.RawQuery, string(body), r.Header.Clone(), err}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(upstream.Close)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path+"?key=gateway-key", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Authorization", "Bearer gateway-key")
			request, err := dto.ParseDecisionsRequest([]byte(tt.body), tt.format)
			require.NoError(t, err)
			common.SetContextKey(c, constant.ContextKeyChannelType, tt.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "fictional-upstream-key")
			common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{OA2GeminiEnabled: true, OA2BaseURLGemini: upstream.URL, OA2OpenAIEnabled: true, OA2BaseURLOpenAI: upstream.URL})
			info, err := relaycommon.GenRelayInfo(c, tt.format, request, nil)
			require.NoError(t, err)
			info.InitChannelMeta(c)
			adaptor := GetAdaptor(info.ApiType)
			require.NotNil(t, adaptor)
			adaptor.Init(info)
			converter, ok := adaptor.(channel.DecisionsConverter)
			require.True(t, ok)
			converted, err := converter.ConvertDecisionsRequest(c, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			response, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			captured := <-received
			require.NoError(t, captured.err)
			assert.Equal(t, tt.path, captured.path)
			assert.Empty(t, captured.query)
			assert.JSONEq(t, tt.body, captured.body)
			if tt.format == types.RelayFormatGeminiDecisions {
				assert.Equal(t, "fictional-upstream-key", captured.header.Get("x-goog-api-key"))
				assert.Empty(t, captured.header.Get("Authorization"))
			} else {
				assert.Equal(t, "Bearer fictional-upstream-key", captured.header.Get("Authorization"))
				assert.Empty(t, captured.header.Get("x-goog-api-key"))
			}
			httpResponse, ok := response.(*http.Response)
			require.True(t, ok)
			usage, apiErr := adaptor.DoResponse(c, httpResponse, info)
			require.Nil(t, apiErr)
			assert.Equal(t, 26, usage.(*dto.Usage).PromptTokens)
			assert.Equal(t, tt.response, recorder.Body.String())
		})
	}
}

func TestDecisionsAdaptorsRejectOtherProviderFormat(t *testing.T) {
	for _, tt := range []struct {
		apiType int
		format  types.RelayFormat
		request dto.DecisionsRequest
	}{
		{constant.APITypeGemini, types.RelayFormatOpenAIDecisions, &dto.OpenAIDecisionsRequest{}},
		{constant.APITypeOpenAI, types.RelayFormatGeminiDecisions, &dto.GeminiDecisionsRequest{}},
	} {
		adaptor := GetAdaptor(tt.apiType)
		require.NotNil(t, adaptor)
		converter, ok := adaptor.(channel.DecisionsConverter)
		require.True(t, ok)
		_, err := converter.ConvertDecisionsRequest(nil, &relaycommon.RelayInfo{RelayFormat: tt.format, ChannelMeta: &relaycommon.ChannelMeta{}}, tt.request)
		require.Error(t, err)
	}
}

func TestDecisionsRequestURLAcceptsVersionedAndFullBaseURLs(t *testing.T) {
	for _, tt := range []struct {
		apiType int
		format  types.RelayFormat
		path    string
	}{
		{constant.APITypeGemini, types.RelayFormatGeminiDecisions, "/v1beta/decisions"},
		{constant.APITypeOpenAI, types.RelayFormatOpenAIDecisions, "/v1/decisions"},
	} {
		for _, baseURL := range []string{"https://gateway.example.com", "https://gateway.example.com/", "https://gateway.example.com" + strings.TrimSuffix(tt.path, "/decisions") + "/", "https://gateway.example.com" + tt.path + "/"} {
			adaptor := GetAdaptor(tt.apiType)
			requestURL, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{RelayFormat: tt.format, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL}})
			require.NoError(t, err)
			assert.Equal(t, "https://gateway.example.com"+tt.path, requestURL)
		}
	}
}
