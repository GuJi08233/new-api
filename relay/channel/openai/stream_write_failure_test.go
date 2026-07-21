package openai

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

type failingOpenAIStreamWriter struct {
	gin.ResponseWriter
	err        error
	writeCalls int
}

func (w *failingOpenAIStreamWriter) Write([]byte) (int, error) {
	w.writeCalls++
	return 0, w.err
}

func (w *failingOpenAIStreamWriter) WriteString(string) (int, error) {
	w.writeCalls++
	return 0, w.err
}

type blockingOpenAISSEBody struct {
	payload   []byte
	readOnce  sync.Once
	closeOnce sync.Once
	closed    chan struct{}
}

func newBlockingOpenAISSEBody(payload string) *blockingOpenAISSEBody {
	return &blockingOpenAISSEBody{
		payload: []byte(payload),
		closed:  make(chan struct{}),
	}
}

func (b *blockingOpenAISSEBody) Read(p []byte) (int, error) {
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

func (b *blockingOpenAISSEBody) Close() error {
	b.closeOnce.Do(func() {
		close(b.closed)
	})
	return nil
}

func newFailingOpenAIStreamContext(t *testing.T, writeErr error) (*gin.Context, *failingOpenAIStreamWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "stream-write-failure")
	writer := &failingOpenAIStreamWriter{ResponseWriter: c.Writer, err: writeErr}
	c.Writer = writer
	return c, writer
}

func newOpenAIStreamWriteRelayInfo(relayMode int, relayFormat types.RelayFormat) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:           true,
		RelayMode:          relayMode,
		RelayFormat:        relayFormat,
		DisablePing:        true,
		OriginModelName:    "stream-test",
		ShouldIncludeUsage: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "stream-test",
		},
	}
}

func TestOaiStreamHandlerStopsAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingOpenAIStreamContext(t, writeErr)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	body := newBlockingOpenAISSEBody(strings.Repeat("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n", 4))

	usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.Equal(t, 1, writer.writeCalls)
	select {
	case <-body.closed:
	default:
		t.Fatal("upstream body was not closed after the fatal write failure")
	}
}

func TestOaiResponsesStreamHandlerStopsAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingOpenAIStreamContext(t, writeErr)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses)
	body := newBlockingOpenAISSEBody(strings.Repeat("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n", 4))

	usage, apiErr := OaiResponsesStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.Equal(t, 1, writer.writeCalls)
}

func TestOaiChatToResponsesStreamHandlerReturnsUsageAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingOpenAIStreamContext(t, writeErr)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses)
	body := newBlockingOpenAISSEBody(strings.Repeat("data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"stream-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n", 4))

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.Equal(t, 1, writer.writeCalls)
}

func TestOaiResponsesToChatStreamHandlerReturnsUsageAfterDownstreamWriteFailure(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c, writer := newFailingOpenAIStreamContext(t, writeErr)
	info := newOpenAIStreamWriteRelayInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	body := newBlockingOpenAISSEBody(strings.Repeat("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n", 4))

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, &http.Response{Body: body})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Positive(t, usage.CompletionTokens)
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.Equal(t, 1, writer.writeCalls)
}
