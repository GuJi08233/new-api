package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesRejectsBackgroundWithoutAsyncSettlement(t *testing.T) {
	for _, field := range []string{"", `,"background":false`, `,"background":null`, `,"background":true`} {
		t.Run(field, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test","input":"hi"`+field+`}`))
			c.Request.Header.Set("Content-Type", "application/json")
			request, err := GetAndValidateResponsesRequest(c)
			if strings.Contains(field, "true") {
				require.ErrorContains(t, err, "background")
				assert.Nil(t, request)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.Equal(t, "test", request.Model)
		})
	}
}
