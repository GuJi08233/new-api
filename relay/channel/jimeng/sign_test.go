package jimeng

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignUsesOverriddenRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	req, err := http.NewRequest(http.MethodPost, "https://original.example.com/?Action=CVProcess", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Host = "override.example.com"
	req.Header.Set("Content-Type", "application/json")

	require.NoError(t, Sign(c, req, "access-key|secret-key"))
	assert.Equal(t, "override.example.com", req.Host)
	assert.Equal(t, "override.example.com", req.Header.Get("Host"))
	assert.Contains(t, req.Header.Get("Authorization"), "SignedHeaders=content-type;host;x-content-sha256;x-date")
}
