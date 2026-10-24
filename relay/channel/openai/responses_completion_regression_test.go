package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completionStreamBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *completionStreamBody) Read(data []byte) (int, error) {
	n, err := b.Reader.Read(data)
	if err == io.EOF {
		<-b.closed
		return 0, io.ErrClosedPipe
	}
	return n, err
}

func (b *completionStreamBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

type completionStreamWriter struct {
	*httptest.ResponseRecorder
	cancel   context.CancelFunc
	writeErr error
}

func (w *completionStreamWriter) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseRecorder.Write(data)
}

func (w *completionStreamWriter) FlushError() error {
	w.ResponseRecorder.Flush()
	if w.cancel != nil {
		w.cancel()
	}
	return nil
}

func TestResponsesCompletionClosesUpstreamAndRetainsUsage(t *testing.T) {
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name      string
		terminal  bool
		cancel    bool
		writeFail bool
		want      relaycommon.StreamEndReason
	}{
		{name: "completion without upstream EOF", terminal: true, want: relaycommon.StreamEndReasonDone},
		{name: "cancellation during final flush", terminal: true, cancel: true, want: relaycommon.StreamEndReasonDone},
		{name: "midstream cancellation remains failed", cancel: true, want: relaycommon.StreamEndReasonClientGone},
		{name: "failed final write remains failed", terminal: true, writeFail: true, want: relaycommon.StreamEndReasonHandlerStop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := `{"type":"response.output_text.delta","delta":"hello"}`
			if tc.terminal {
				payload = `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`
			}
			body := &completionStreamBody{Reader: strings.NewReader("data: " + payload + "\n\n"), closed: make(chan struct{})}
			t.Cleanup(func() { require.NoError(t, body.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &completionStreamWriter{ResponseRecorder: httptest.NewRecorder()}
			if tc.cancel {
				writer.cancel = cancel
			}
			if tc.writeFail {
				writer.writeErr = errors.New("downstream failure")
			}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"}}
			var usage *dto.Usage
			var relayErr *types.NewAPIError
			done := make(chan struct{})
			go func() {
				usage, relayErr = OaiResponsesStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)})
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not finish after completion or cancellation")
			}
			require.Nil(t, relayErr)
			assert.Equal(t, tc.want, info.StreamStatus.EndReason)
			if tc.terminal {
				require.NotNil(t, usage)
				assert.Equal(t, 7, usage.PromptTokens)
				assert.Equal(t, 5, usage.CompletionTokens)
			}
			if tc.writeFail {
				assert.True(t, info.StreamStatus.HasErrors())
			}
			select {
			case <-body.closed:
			default:
				t.Error("upstream body was not closed")
			}
		})
	}
}
