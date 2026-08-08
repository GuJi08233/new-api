package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsEndpointAllowedGeminiProtectedRoute(t *testing.T) {
	configuredEndpoints := parseEndpoints(`{
		"gemini": {
			"path": "/v1beta/models/{model}:generateContent",
			"method": "POST"
		}
	}`)
	require.Equal(t, []string{"/v1beta/models/{model}:generateContent"}, configuredEndpoints)

	tests := []struct {
		name        string
		requestPath string
		want        bool
	}{
		{
			name:        "v1 streaming route",
			requestPath: "/v1/models/gemini-3.5-flash-lite:streamGenerateContent",
			want:        true,
		},
		{
			name:        "v1beta non-streaming route",
			requestPath: "/v1beta/models/gemini-3.5-flash-lite:generateContent",
			want:        true,
		},
		{
			name:        "different Gemini action",
			requestPath: "/v1beta/models/gemini-3.5-flash-lite:countTokens",
			want:        false,
		},
		{
			name:        "unrelated route",
			requestPath: "/v1/chat/completions",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEndpointAllowed(tt.requestPath, configuredEndpoints))
		})
	}
}

func TestIsEndpointAllowedEmbeddingAndRerank(t *testing.T) {
	tests := []struct {
		name    string
		request string
		allowed []string
		want    bool
	}{
		{name: "embedding exact", request: "/v1/embeddings", allowed: []string{"/v1/embeddings"}, want: true},
		{name: "embedding trailing slash", request: "/v1/embeddings/", allowed: []string{"/v1/embeddings"}, want: true},
		{name: "embedding cannot use rerank", request: "/v1/embeddings", allowed: []string{"/v1/rerank"}, want: false},
		{name: "rerank exact", request: "/v1/rerank", allowed: []string{"/v1/rerank"}, want: true},
		{name: "rerank cannot use embedding", request: "/v1/rerank", allowed: []string{"/v1/embeddings"}, want: false},
		{name: "gemini single embedding", request: "/v1beta/models/embed-model:embedContent", allowed: []string{"/v1beta/models/{model}:embedContent"}, want: true},
		{name: "gemini batch embedding", request: "/v1beta/models/embed-model:batchEmbedContents", allowed: []string{"/v1beta/models/{model}:batchEmbedContents"}, want: true},
		{name: "gemini batch differs from single", request: "/v1beta/models/embed-model:batchEmbedContents", allowed: []string{"/v1beta/models/{model}:embedContent"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEndpointAllowed(tt.request, tt.allowed))
		})
	}
}

func TestCheckModelEndpointProtectionSeparatesEmbeddingAndRerankModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Model{}))
	require.NoError(t, db.Create([]model.Model{
		{
			ModelName: "embedding-model",
			Endpoints: `{"embeddings":{"path":"/v1/embeddings","method":"POST"}}`,
			Status:    1,
		},
		{
			ModelName: "rerank-model",
			Endpoints: `{"jina-rerank":{"path":"/v1/rerank","method":"POST"}}`,
			Status:    1,
		},
	}).Error)

	originalDB := model.DB
	originalReadOnlyDB := model.RO_DB
	model.DB = db
	model.RO_DB = nil
	t.Cleanup(func() {
		model.DB = originalDB
		model.RO_DB = originalReadOnlyDB
	})

	settings := model_setting.GetGlobalSettings()
	originalEnabled := settings.ModelEndpointProtectEnabled
	settings.ModelEndpointProtectEnabled = true
	t.Cleanup(func() { settings.ModelEndpointProtectEnabled = originalEnabled })

	tests := []struct {
		name        string
		modelName   string
		requestPath string
		wantAllowed bool
	}{
		{name: "embedding model on embeddings", modelName: "embedding-model", requestPath: "/v1/embeddings", wantAllowed: true},
		{name: "embedding model blocked on rerank", modelName: "embedding-model", requestPath: "/v1/rerank", wantAllowed: false},
		{name: "rerank model on rerank", modelName: "rerank-model", requestPath: "/v1/rerank", wantAllowed: true},
		{name: "rerank model blocked on embeddings", modelName: "rerank-model", requestPath: "/v1/embeddings", wantAllowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, tt.requestPath, nil)

			endpointErr := checkModelEndpointProtection(ctx, tt.modelName, tt.requestPath)
			if tt.wantAllowed {
				require.Nil(t, endpointErr)
				return
			}
			require.NotNil(t, endpointErr)
			assert.Equal(t, http.StatusForbidden, endpointErr.StatusCode)
			assert.Contains(t, endpointErr.Error(), tt.requestPath)
		})
	}
}

func TestParseEndpointsSupportsAliasesAndLegacyFormats(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "legacy array",
			input: `["embeddings", "jina-rerank", "gemini-embed"]`,
			want:  []string{"/v1/embeddings", "/v1/rerank", "/v1beta/models/{model}:embedContent"},
		},
		{
			name:  "comma separated aliases",
			input: "embeddings, jina-rerank",
			want:  []string{"/v1/embeddings", "/v1/rerank"},
		},
		{
			name:  "single descriptor",
			input: `{"path":"/v1/embeddings","method":"POST"}`,
			want:  []string{"/v1/embeddings"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints, valid := parseEndpointsWithStatus(tt.input)
			require.True(t, valid)
			assert.ElementsMatch(t, tt.want, endpoints)
		})
	}
}

func TestParseEndpointsRejectsMalformedNonEmptyJSON(t *testing.T) {
	endpoints, valid := parseEndpointsWithStatus(`{"embedding":`)
	assert.False(t, valid)
	assert.Empty(t, endpoints)

	endpoints, valid = parseEndpointsWithStatus(`{}`)
	assert.True(t, valid)
	assert.Empty(t, endpoints)
}

func TestChannelRouteRejectedErrorUsesClientErrorSemantics(t *testing.T) {
	err := newChannelRouteRejectedError(nil, "rerank-model", &service.ChannelRouteMatch{
		RuleName:      "docs-limit",
		RejectMessage: "too many documents",
	})

	assert.Equal(t, 400, err.StatusCode)
	assert.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(err))
	assert.Equal(t, "too many documents", err.Error())
}

func TestRerouteByRequestMetricsSkipsExplicitChannelSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("specific_channel_id", "42")

	err := rerouteByRequestMetrics(ctx, &relaycommon.RelayInfo{TokenGroup: "default", OriginModelName: "model"})
	require.Nil(t, err)
}
