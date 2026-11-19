package channel_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPassthroughSendsOnlyClientHeadersAndProviderCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	global := model_setting.GetGlobalSettings()
	oldHeaders, oldBody := global.PassThroughHeadersEnabled, global.PassThroughRequestEnabled
	global.PassThroughHeadersEnabled, global.PassThroughRequestEnabled = false, false
	t.Cleanup(func() {
		global.PassThroughHeadersEnabled, global.PassThroughRequestEnabled = oldHeaders, oldBody
	})

	for _, tc := range []struct {
		name        string
		adaptor     channel.Adaptor
		channelType int
		path        string
		format      types.RelayFormat
		encoding    string
		authHeader  string
		authValue   string
	}{
		{"OpenAI", &openai.Adaptor{}, constant.ChannelTypeOpenAI, "/v1/chat/completions?client_option=1", types.RelayFormatOpenAI, "", "Authorization", "Bearer upstream-key"},
		{"OpenRouter", &openai.Adaptor{}, constant.ChannelTypeOpenRouter, "/v1/chat/completions", types.RelayFormatOpenAI, "", "Authorization", "Bearer upstream-key"},
		{"Claude", &claude.Adaptor{}, constant.ChannelTypeAnthropic, "/v1/messages?beta=true&client_option=1", types.RelayFormatClaude, "", "X-Api-Key", "upstream-key"},
		{"Claude via OpenAI compatible", &openai.Adaptor{}, constant.ChannelTypeOpenAI, "/v1/messages?beta=true", types.RelayFormatClaude, "", "Authorization", "Bearer upstream-key"},
		{"gzip request", &openai.Adaptor{}, constant.ChannelTypeOpenAI, "/v1/chat/completions", types.RelayFormatOpenAI, "gzip", "Authorization", "Bearer upstream-key"},
		{"brotli request", &openai.Adaptor{}, constant.ChannelTypeOpenAI, "/v1/chat/completions", types.RelayFormatOpenAI, "br", "Authorization", "Bearer upstream-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("{\n \"model\":\"client-model\", \"messages\":[], \"unknown\":false, \"zero\":0\n}")
			wireBody := raw
			if tc.encoding != "" {
				var compressed bytes.Buffer
				var writer io.WriteCloser
				if tc.encoding == "gzip" {
					writer = gzip.NewWriter(&compressed)
				} else {
					writer = brotli.NewWriter(&compressed)
				}
				_, err := writer.Write(raw)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				wireBody = compressed.Bytes()
			}
			type capturedRequest struct {
				headers http.Header
				body    []byte
				path    string
				err     error
			}
			received := make(chan capturedRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- capturedRequest{r.Header.Clone(), body, r.URL.RequestURI(), err}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2}}`))
			}))
			defer upstream.Close()

			router := gin.New()
			router.Use(middleware.DecompressRequestMiddleware())
			router.POST("/*path", func(c *gin.Context) {
				defer common.CleanupBodyStorage(c)
				var parsed map[string]any
				require.NoError(t, common.UnmarshalBodyReusable(c, &parsed))
				assert.Equal(t, "client-model", parsed["model"])
				info := &relaycommon.RelayInfo{
					RelayFormat: tc.format, RequestURLPath: c.Request.URL.RequestURI(),
					IsStream: true, OriginModelName: "client-model",
					ChannelMeta: &relaycommon.ChannelMeta{
						ChannelType: tc.channelType, ChannelBaseUrl: upstream.URL,
						ApiKey: "upstream-key", Organization: "adaptor-default-organization",
						IsModelMapped: true, UpstreamModelName: "must-not-rewrite",
						ChannelSetting: dto.ChannelSettings{
							PassThroughBodyEnabled: true, PassThroughHeadersEnabled: true,
						},
						HeadersOverride: map[string]any{"X-Explicit": "operator-value"},
					},
				}
				body, err := relaycommon.GetPassThroughRequestBody(c, info)
				require.NoError(t, err)
				resp, err := channel.DoApiRequest(tc.adaptor, c, info, body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
			})
			request := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(wireBody))
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
			request.Header.Set("Authorization", "Bearer gateway-key")
			request.Header.Set("X-Api-Key", "gateway-key")
			request.Header.Set("CF-Ray", "must-not-forward")
			request.Header.Set("X-Forwarded-For", "192.0.2.1")
			request.Header.Set("New-Api-User", "42")
			request.Header.Set("Cookie", "gateway=session")
			request.Header.Add("Anthropic-Beta", "beta-a")
			request.Header.Add("Anthropic-Beta", "beta-b")
			request.Header.Set("X-Empty", "")
			if tc.encoding != "" {
				request.Header.Set("Content-Encoding", tc.encoding)
			}
			router.ServeHTTP(httptest.NewRecorder(), request)
			got := <-received
			require.NoError(t, got.err)
			assert.Equal(t, wireBody, got.body)
			assert.Equal(t, tc.path, got.path)
			expected := http.Header{
				"Content-Type":   {"application/json; charset=utf-8"},
				"Content-Length": {strconv.Itoa(len(wireBody))},
				"Anthropic-Beta": {"beta-a", "beta-b"},
				"X-Empty":        {""},
				"X-Explicit":     {"operator-value"},
				tc.authHeader:    {tc.authValue},
			}
			if tc.encoding != "" {
				expected.Set("Content-Encoding", tc.encoding)
			}
			assert.Equal(t, expected, got.headers)
		})
	}
}
