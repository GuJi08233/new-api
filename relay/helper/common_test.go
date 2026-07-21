package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
