package helper

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type observingResponseWriter struct {
	gin.ResponseWriter
	writes chan string
}

func (w *observingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.writes <- string(p)
	return n, err
}

func (w *observingResponseWriter) WriteString(value string) (int, error) {
	n, err := w.ResponseWriter.WriteString(value)
	w.writes <- value
	return n, err
}

func newStreamSessionTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *observingResponseWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	writer := &observingResponseWriter{
		ResponseWriter: c.Writer,
		writes:         make(chan string, 32),
	}
	c.Writer = writer
	return c, recorder, writer
}

func TestStartStreamSessionCopiesHeadersBeforePing(t *testing.T) {
	c, recorder, writer := newStreamSessionTestContext(t)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Upstream-Trace": []string{"trace-value"}},
		Body:       http.NoBody,
	}
	ticks := make(chan time.Time, 1)
	stop := startStreamSession(c, info, resp, ticks, nil, nil, nil)
	require.NotNil(t, streamSessionFromContext(c))

	assert.Equal(t, "trace-value", c.Writer.Header().Get("X-Upstream-Trace"),
		"upstream metadata must be installed before a ping can flush headers")
	ticks <- time.Now()
	select {
	case frame := <-writer.writes:
		assert.Equal(t, ": PING\n\n", frame)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream ping")
	}
	assert.Equal(t, ": PING\n\n", recorder.Body.String())

	stop()
	stop()
	assert.Nil(t, streamSessionFromContext(c))
}

func TestStartStreamSessionWithoutPingStopsSynchronously(t *testing.T) {
	c, _, _ := newStreamSessionTestContext(t)
	stop := startStreamSession(c, nil, nil, nil, nil, nil, nil)
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream session stop blocked without a pinger")
	}
	assert.Nil(t, streamSessionFromContext(c))
}

func TestWithStreamWriteSerializesBusinessAndPing(t *testing.T) {
	c, _, _ := newStreamSessionTestContext(t)
	stop := startStreamSession(c, nil, nil, nil, nil, nil, nil)
	defer stop()

	events := make(chan string, 3)
	businessEntered := make(chan struct{})
	releaseBusiness := make(chan struct{})
	var once sync.Once
	go func() {
		_ = withStreamWrite(c, func() error {
			events <- "business-start"
			once.Do(func() { close(businessEntered) })
			<-releaseBusiness
			events <- "business-end"
			return nil
		})
	}()
	<-businessEntered

	pingDone := make(chan struct{})
	go func() {
		_ = withStreamWrite(c, func() error {
			events <- "ping"
			return nil
		})
		close(pingDone)
	}()

	close(releaseBusiness)
	select {
	case <-pingDone:
	case <-time.After(time.Second):
		t.Fatal("serialized stream write did not complete")
	}

	assert.Equal(t, []string{"business-start", "business-end", "ping"},
		[]string{<-events, <-events, <-events})
}

func TestStopStreamSessionWaitsForInFlightPingAndPreventsLaterPing(t *testing.T) {
	c, _, _ := newStreamSessionTestContext(t)
	ticks := make(chan time.Time, 2)
	pingStarted := make(chan struct{})
	releasePing := make(chan struct{})
	pingCalls := make(chan struct{}, 2)
	stop := startStreamSession(c, nil, nil, ticks, nil, func() error {
		pingCalls <- struct{}{}
		close(pingStarted)
		<-releasePing
		return nil
	}, nil)

	ticks <- time.Now()
	select {
	case <-pingStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for in-flight ping")
	}

	stopped := make(chan struct{})
	stopStarted := make(chan struct{})
	go func() {
		close(stopStarted)
		stop()
		close(stopped)
	}()
	<-stopStarted
	select {
	case <-stopped:
		t.Fatal("stop returned before the in-flight ping completed")
	default:
	}

	close(releasePing)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not wait for the ping goroutine to exit")
	}

	select {
	case <-pingCalls:
	default:
		t.Fatal("expected the in-flight ping to run exactly once")
	}
	ticks <- time.Now()
	select {
	case <-pingCalls:
		t.Fatal("ping ran after the stream session stopped")
	default:
	}
}

func TestDonePreventsLaterPing(t *testing.T) {
	c, recorder, writer := newStreamSessionTestContext(t)
	ticks := make(chan time.Time, 1)
	stop := startStreamSession(c, nil, nil, ticks, nil, nil, nil)
	defer stop()

	Done(c)
	assert.Equal(t, "data: [DONE]\n\n", recorder.Body.String())

drainTerminalWrites:
	for {
		select {
		case <-writer.writes:
		default:
			break drainTerminalWrites
		}
	}

	ticks <- time.Now()
	select {
	case frame := <-writer.writes:
		t.Fatalf("unexpected write after terminal frame: %q", frame)
	case <-time.After(50 * time.Millisecond):
	}
	assert.Equal(t, "data: [DONE]\n\n", recorder.Body.String())
	stop()
	assert.Nil(t, streamSessionFromContext(c), "a normal terminal frame must not retain a failed session")
}

type closeTrackingBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *closeTrackingBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (b *closeTrackingBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestPingFailureAbortsUpstreamAndBlocksTerminalWrite(t *testing.T) {
	c, recorder, _ := newStreamSessionTestContext(t)
	ticks := make(chan time.Time, 1)
	pingErr := errors.New("downstream write failed")
	body := &closeTrackingBody{closed: make(chan struct{})}
	aborted := make(chan struct{})
	stop := startStreamSession(c, nil, &http.Response{Body: body}, ticks, nil, func() error {
		return pingErr
	}, func() {
		close(aborted)
	})

	ticks <- time.Now()
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream response body to close")
	}
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider abort callback")
	}

	stop()
	session := streamSessionFromContext(c)
	require.NotNil(t, session, "failed sessions must remain installed to reject final writes")
	assert.True(t, session.failed)
	assert.ErrorIs(t, session.failure, pingErr)
	assert.ErrorIs(t, StreamSessionWriteError(c), pingErr)
	assert.ErrorIs(t, StringData(c, "must-not-write"), pingErr)
	Done(c)
	assert.Empty(t, recorder.Body.String())
}
