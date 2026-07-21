package gemini

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingGeminiStreamWriter struct {
	gin.ResponseWriter
	err        error
	writeCalls int
}

func (w *failingGeminiStreamWriter) Write([]byte) (int, error) {
	w.writeCalls++
	return 0, w.err
}

func (w *failingGeminiStreamWriter) WriteString(string) (int, error) {
	w.writeCalls++
	return 0, w.err
}

type blockingGeminiSSEBody struct {
	payload   []byte
	readOnce  sync.Once
	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingGeminiSSEBody(payload string) *blockingGeminiSSEBody {
	return &blockingGeminiSSEBody{
		payload: []byte(payload),
		closed:  make(chan struct{}),
	}
}

func (b *blockingGeminiSSEBody) Read(p []byte) (int, error) {
	n := 0
	b.readOnce.Do(func() {
		n = copy(p, b.payload)
	})
	if n > 0 {
		return n, nil
	}
	<-b.closed
	return 0, io.EOF
}

func (b *blockingGeminiSSEBody) Close() error {
	b.closeOnce.Do(func() {
		close(b.closed)
	})
	return nil
}

func newFailingGeminiStreamContext(t *testing.T, writeErr error) (*gin.Context, *failingGeminiStreamWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "gemini-stream-write-failure")
	writer := &failingGeminiStreamWriter{ResponseWriter: c.Writer, err: writeErr}
	c.Writer = writer
	return c, writer
}

func newGeminiStreamWriteRelayInfo(relayFormat types.RelayFormat) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:        true,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     relayFormat,
		DisablePing:     true,
		OriginModelName: "gemini-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-test",
		},
	}
}

func TestGeminiChatStreamHandlerStopsAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingGeminiStreamContext(t, writeErr)
	info := newGeminiStreamWriteRelayInfo(types.RelayFormatOpenAI)
	body := newBlockingGeminiSSEBody(strings.Repeat("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}]}\n\n", 4))

	usage, apiErr := GeminiChatStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, writeErr)
	assert.Equal(t, 1, writer.writeCalls)
	select {
	case <-body.closed:
	default:
		t.Fatal("upstream body was not closed after the fatal write failure")
	}
}

func TestGeminiResponsesStreamHandlerStopsAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingGeminiStreamContext(t, writeErr)
	info := newGeminiStreamWriteRelayInfo(types.RelayFormatOpenAIResponses)
	info.RelayMode = relayconstant.RelayModeResponses
	body := newBlockingGeminiSSEBody(strings.Repeat("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}]}\n\n", 4))

	usage, apiErr := GeminiResponsesStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, writeErr)
	assert.Equal(t, 1, writer.writeCalls)
}

func TestGeminiNativeStreamHandlerPreservesDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingGeminiStreamContext(t, writeErr)
	info := newGeminiStreamWriteRelayInfo(types.RelayFormatGemini)
	body := newBlockingGeminiSSEBody(strings.Repeat("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]}}]}\n\n", 4))

	usage, apiErr := GeminiTextGenerationStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, writeErr)
	assert.Equal(t, 1, writer.writeCalls)
	select {
	case <-body.closed:
	default:
		t.Fatal("upstream body was not closed after the fatal write failure")
	}
}
