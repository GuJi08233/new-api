package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDocumentRouteTestContext(t *testing.T, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(body))
	require.NoError(t, err)
	ctx.Set(common.KeyBodyStorage, storage)
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx, recorder
}

func TestGetModelRequestEstimatesRerankDocuments(t *testing.T) {
	for _, count := range []int{0, 25, 26, 200, 201} {
		t.Run(fmt.Sprintf("%d documents", count), func(t *testing.T) {
			documents := make([]string, count)
			for index := range documents {
				documents[index] = fmt.Sprintf("doc-%d", index)
			}
			body, err := common.Marshal(map[string]any{
				"model":     "rerank-model",
				"documents": documents,
			})
			require.NoError(t, err)
			ctx, _ := newDocumentRouteTestContext(t, "/v1/rerank/", string(body))

			request, _, err := getModelRequest(ctx)
			require.NoError(t, err)
			assert.Equal(t, "rerank-model", request.Model)
			assert.Equal(t, count, common.GetContextKeyInt(ctx, constant.ContextKeyEstimatedDocs))
		})
	}
}

func TestGetModelRequestEstimatesEmbeddingInputs(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "single string", path: "/v1/embeddings", body: `{"model":"embed-model","input":"hello"}`, want: 1},
		{name: "string batch with trailing slash", path: "/v1/embeddings/", body: `{"model":"embed-model","input":["a","b","c"]}`, want: 3},
		{name: "flat token input", path: "/v1/embeddings", body: `{"model":"embed-model","input":[1,2,3]}`, want: 1},
		{name: "token batch", path: "/v1/embeddings", body: `{"model":"embed-model","input":[[1,2],[3,4],[5,6]]}`, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := newDocumentRouteTestContext(t, tt.path, tt.body)
			request, _, err := getModelRequest(ctx)
			require.NoError(t, err)
			assert.Equal(t, "embed-model", request.Model)
			assert.Equal(t, tt.want, common.GetContextKeyInt(ctx, constant.ContextKeyEstimatedDocs))
		})
	}
}

func TestGetModelRequestEstimatesNativeGeminiEmbeddingInputs(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{
			name: "single embedContent",
			path: "/v1beta/models/embed-model:embedContent",
			body: `{"content":{"parts":[{"text":"hello"}]}}`,
			want: 1,
		},
		{
			name: "batch embedContents",
			path: "/v1beta/models/embed-model:batchEmbedContents",
			body: `{"requests":[{"content":{"parts":[{"text":"a"}]}},{"content":{"parts":[{"text":"b"}]}}]}`,
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := newDocumentRouteTestContext(t, tt.path, tt.body)
			_, _, err := getModelRequest(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, common.GetContextKeyInt(ctx, constant.ContextKeyEstimatedDocs))
		})
	}
}

func TestDistributeRejectTierReturnsCustomClientErrorWithoutRouting(t *testing.T) {
	cfg := operation_setting.GetChannelRouteSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
	})
	cfg.Enabled = true
	cfg.Rules = []operation_setting.ChannelRouteRule{{
		Name:       "small-only-rerank",
		ModelRegex: []string{"^rerank-model$"},
		PathRegex:  []string{"^/v1/rerank$"},
		Strict:     true,
		RouteTiers: []operation_setting.RouteTier{
			{
				Conditions: []operation_setting.RouteTierCondition{{Var: "docs", Op: "<=", Value: 25}},
				ChannelIDs: []int{1, 2},
			},
			{
				Conditions:    []operation_setting.RouteTierCondition{{Var: "docs", Op: ">", Value: 25}},
				Reject:        true,
				RejectMessage: "最多只能提交 25 条候选文档",
			},
		},
	}}

	documents := make([]string, 26)
	for index := range documents {
		documents[index] = fmt.Sprintf("doc-%d", index)
	}
	body, err := common.Marshal(map[string]any{
		"model":     "rerank-model",
		"documents": documents,
	})
	require.NoError(t, err)

	ctx, recorder := newDocumentRouteTestContext(t, "/v1/rerank", string(body))
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	ctx.Set(common.RequestIdKey, "route-test")

	Distribute()(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var response struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "最多只能提交 25 条候选文档 (request id: route-test)", response.Error.Message)
	assert.Equal(t, string(types.ErrorCodeInvalidRequest), response.Error.Code)
}
