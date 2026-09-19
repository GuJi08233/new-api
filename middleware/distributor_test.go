package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteWillEstimateTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		path      string
		relayMode int
		want      bool
	}{
		{name: "chat relay", path: "/v1/chat/completions", want: true},
		{name: "claude messages relay", path: "/v1/messages", want: true},
		{name: "openai embeddings relay", path: "/v1/embeddings", want: true},
		{name: "rerank relay", path: "/v1/rerank", want: true},
		{name: "gemini generation relay", path: "/v1beta/models/gemini-2.5-pro:generateContent", want: true},
		{name: "gemini embedding relay", path: "/v1beta/models/embed-model:embedContent", want: true},
		{name: "gemini batch embedding relay", path: "/v1beta/models/embed-model:batchEmbedContents", want: true},
		{name: "midjourney task", path: "/mj/submit/imagine", want: false},
		{name: "suno task", path: "/suno/submit/music", relayMode: relayconstant.RelayModeSunoSubmit, want: false},
		{name: "video task", path: "/v1/videos", relayMode: relayconstant.RelayModeVideoSubmit, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", tt.path, nil)
			if tt.relayMode != relayconstant.RelayModeUnknown {
				ctx.Set("relay_mode", tt.relayMode)
			}
			assert.Equal(t, tt.want, routeWillEstimateTokens(ctx))
		})
	}
}

// 路由、令牌模型限制和分组鉴权读的是 gjson 的取值（重复键取第一个），而请求体随后
// 又会被 encoding/json 解析：它按大小写折叠匹配字段名并取最后一个。两个解析器看到不同
// 的值就等于让鉴权与实际执行分离，所以重复或大小写变体的 model/group 必须被拒绝。
func TestGetModelFromJSONBodyRejectsConflictingRoutingKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		body      string
		wantErr   string
		wantModel string
		wantGroup string
	}{
		{
			name:    "duplicate model",
			body:    `{"model":"gpt-3.5-turbo","messages":[],"model":"gpt-4o"}`,
			wantErr: "conflicting model field in JSON request body",
		},
		{
			name:    "duplicate group",
			body:    `{"group":"vip","model":"gpt-4o","group":"default"}`,
			wantErr: "conflicting group field in JSON request body",
		},
		{
			// encoding/json 会把 Model 折叠到同一个字段并取后者，透传时上游收到的也是它
			name:    "case variant model shadows routed model",
			body:    `{"model":"allowed","Model":"restricted","messages":[]}`,
			wantErr: "conflicting model field in JSON request body",
		},
		{
			name:    "case variant group shadows routed group",
			body:    `{"model":"gpt-4o","GROUP":"vip"}`,
			wantErr: "conflicting group field in JSON request body",
		},
		{
			name:    "case variant alone is still rejected",
			body:    `{"Model":"restricted","messages":[]}`,
			wantErr: "conflicting model field in JSON request body",
		},
		{
			name:      "unique keys accepted",
			body:      `{"model":"gpt-4o","group":"default"}`,
			wantModel: "gpt-4o",
			wantGroup: "default",
		},
		{
			name:      "nested model key is not a duplicate",
			body:      `{"model":"gpt-4o","metadata":{"model":"gpt-4o-mini"}}`,
			wantModel: "gpt-4o",
		},
		{
			name: "absent keys stay empty",
			body: `{"messages":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			got, err := getModelFromJSONBody(ctx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantModel, got.Model)
			assert.Equal(t, tt.wantGroup, got.Group)
		})
	}
}

// playground 的分组由 Distribute 鉴权后才写入上下文。TokenGroup 是重试与重路由
// 选渠道的依据（controller/relay.go 的 RetryParam），在这里写入请求里的原始值会让
// 换渠道时绕开分组权限。
func TestGetModelRequestKeepsPlaygroundGroupOutOfContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","group":"vip"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	modelRequest, shouldSelectChannel, err := getModelRequest(ctx)
	require.NoError(t, err)
	assert.True(t, shouldSelectChannel)
	assert.Equal(t, "vip", modelRequest.Group)

	_, written := common.GetContextKey(ctx, constant.ContextKeyTokenGroup)
	assert.False(t, written, "unauthorized playground group must not reach ContextKeyTokenGroup")
}
