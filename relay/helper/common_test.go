package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type errorAwareHTTPWriter struct {
	header   http.Header
	writeErr error
	flushErr error
}

func (w *errorAwareHTTPWriter) Header() http.Header {
	return w.header
}

func (w *errorAwareHTTPWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}

func (w *errorAwareHTTPWriter) WriteHeader(int) {}

func (w *errorAwareHTTPWriter) FlushError() error {
	return w.flushErr
}

func newErrorAwareStreamContext(w http.ResponseWriter) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func TestStringDataReturnsRenderWriteError(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c := newErrorAwareStreamContext(&errorAwareHTTPWriter{
		header:   make(http.Header),
		writeErr: writeErr,
	})

	err := StringData(c, "payload")

	require.ErrorIs(t, err, writeErr)
	require.True(t, IsStreamWriteError(err))
}

func TestObjectDataMarshalErrorIsNotStreamWriteError(t *testing.T) {
	c := newErrorAwareStreamContext(&errorAwareHTTPWriter{header: make(http.Header)})

	err := ObjectData(c, map[string]any{"invalid": func() {}})

	require.Error(t, err)
	require.False(t, IsStreamWriteError(err))
}

func TestStringDataWriteFailureAbortsStreamSession(t *testing.T) {
	writeErr := errors.New("downstream write failed")
	c := newErrorAwareStreamContext(&errorAwareHTTPWriter{
		header:   make(http.Header),
		writeErr: writeErr,
	})
	aborted := make(chan struct{})
	stop := startStreamSession(c, nil, nil, nil, nil, nil, func() {
		close(aborted)
	})

	err := StringData(c, "payload")

	require.ErrorIs(t, err, writeErr)
	select {
	case <-aborted:
	default:
		t.Fatal("stream session did not abort after a business write failure")
	}
	stop()
	require.NotNil(t, streamSessionFromContext(c))
}

func TestFlushWriterReturnsUnderlyingFlushError(t *testing.T) {
	flushErr := errors.New("downstream flush failed")
	c := newErrorAwareStreamContext(&errorAwareHTTPWriter{
		header:   make(http.Header),
		flushErr: flushErr,
	})

	err := FlushWriter(c)

	require.ErrorIs(t, err, flushErr)
}

func TestStringDataReturnsUnderlyingFlushError(t *testing.T) {
	flushErr := errors.New("downstream flush failed")
	c := newErrorAwareStreamContext(&errorAwareHTTPWriter{
		header:   make(http.Header),
		flushErr: flushErr,
	})

	err := StringData(c, "small-frame")

	require.ErrorIs(t, err, flushErr)
}

func TestResetEventStreamHeadersAllowsJSONErrorAndRetry(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	SetEventStreamHeaders(c)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))

	ResetEventStreamHeaders(c)
	require.Empty(t, recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))

	SetEventStreamHeaders(c)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

// TestStreamExitsRewriteModelName pins that every SSE writer applies the
// response model rewrite, so a redirected request never leaks the upstream
// model name through a stream frame while the option is on.
func TestStreamExitsRewriteModelName(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.RewriteResponseModelEnabled
	t.Cleanup(func() { settings.RewriteResponseModelEnabled = original })
	settings.RewriteResponseModelEnabled = true

	tests := []struct {
		name  string
		write func(c *gin.Context) error
		want  string
	}{
		{
			name: "openai chunk",
			write: func(c *gin.Context) error {
				return StringData(c, `{"object":"chat.completion.chunk","model":"gpt-4o"}`)
			},
			want: `{"object":"chat.completion.chunk","model":"gpt-4"}`,
		},
		{
			name: "claude message_start",
			write: func(c *gin.Context) error {
				return ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_start"},
					`{"type":"message_start","message":{"model":"gpt-4o"}}`)
			},
			want: `{"type":"message_start","message":{"model":"gpt-4"}}`,
		},
		{
			name: "responses event",
			write: func(c *gin.Context) error {
				return ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"},
					`{"type":"response.created","response":{"model":"gpt-4o"}}`)
			},
			want: `{"type":"response.created","response":{"model":"gpt-4"}}`,
		},
		{
			name: "claude object",
			write: func(c *gin.Context) error {
				return ClaudeData(c, dto.ClaudeResponse{Type: "message_delta", Model: "gpt-4o"})
			},
			want: `"model":"gpt-4"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c := newErrorAwareStreamContext(recorder)
			service.SetModelRewrite(c, nil, "gpt-4o", "gpt-4")

			require.NoError(t, tt.write(c))

			body := recorder.Body.String()
			require.Contains(t, body, tt.want)
			require.NotContains(t, body, "gpt-4o")
		})
	}
}
