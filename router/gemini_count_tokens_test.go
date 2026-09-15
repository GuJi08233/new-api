package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGeminiCountTokensDoesNotReachGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetRelayRouter(router)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:countTokens", strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`)))
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}
