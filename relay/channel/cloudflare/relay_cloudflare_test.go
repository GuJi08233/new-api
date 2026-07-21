package cloudflare

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalUsageFailingWriter struct {
	gin.ResponseWriter
	err       error
	writes    int
	failAfter int
}

func (w *finalUsageFailingWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > w.failAfter {
		return 0, w.err
	}
	return w.ResponseWriter.Write(data)
}

func (w *finalUsageFailingWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func TestCFStreamHandlerDoesNotWriteDoneAfterFinalUsageFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "cloudflare-final-usage")
	writeErr := errors.New("downstream write failed")
	writer := &finalUsageFailingWriter{
		ResponseWriter: c.Writer,
		err:            writeErr,
		failAfter:      2,
	}
	c.Writer = writer
	info := &relaycommon.RelayInfo{
		IsStream:           true,
		DisablePing:        true,
		ShouldIncludeUsage: true,
		StartTime:          time.Unix(1, 0),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "cloudflare-test",
		},
	}
	info.SetEstimatePromptTokens(3)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n" +
				"data: [DONE]\n",
		)),
	}

	apiErr, usage := cfStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.TotalTokens)
	assert.Equal(t, 3, writer.writes,
		"a failed final usage frame must not be followed by a terminal frame")
}
