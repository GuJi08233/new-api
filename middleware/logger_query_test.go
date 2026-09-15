package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAccessLoggerDoesNotExposeQueryCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := gin.DefaultWriter
	var out bytes.Buffer
	gin.DefaultWriter = &out
	t.Cleanup(func() { gin.DefaultWriter = original })
	router := gin.New()
	SetUpLogger(router)
	router.GET("/oauth/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/oauth/test?code=secret-code&state=secret-state", nil))
	assert.Contains(t, out.String(), "/oauth/test")
	assert.NotContains(t, out.String(), "secret-code")
	assert.NotContains(t, out.String(), "secret-state")
}
