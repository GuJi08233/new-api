package ali

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliImageUsageRejectsUnboundedCount(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{}, ImageRequestCount: 2}
	err, _ := aliImageHandler(&Adaptor{IsSyncImageModel: true}, c, &http.Response{Body: io.NopCloser(strings.NewReader(`{"usage":{"image_count":2147483647}}`))}, info)
	require.Nil(t, err)
	assert.Equal(t, 2.0, info.PriceData.OtherRatios()["n"])
}
