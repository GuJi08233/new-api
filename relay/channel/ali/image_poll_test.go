package ali

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTaskUsesProxyAndAppliesRuntimeHeaderOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	received := make(chan *http.Request, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED"}}`))
	}))
	defer proxy.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"Authorization": "Bearer custom-token",
			"X-Trace-Id":    "trace-ali",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "http://dashscope.example",
			ApiKey:         "provider-token",
			ChannelSetting: dto.ChannelSettings{Proxy: proxy.URL},
		},
	}

	response, err, body := updateTask(c, info, "task-id")
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, "SUCCEEDED", response.Output.TaskStatus)
	assert.NotEmpty(t, body)

	request := <-received
	assert.Equal(t, "http://dashscope.example/api/v1/tasks/task-id", request.URL.String())
	assert.Equal(t, "Bearer custom-token", request.Header.Get("Authorization"))
	assert.Equal(t, "trace-ali", request.Header.Get("X-Trace-Id"))
}

func TestAsyncTaskWaitReturnsPromptlyWhenCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("initial delay", func(t *testing.T) {
		requestCtx, cancel := context.WithCancel(context.Background())
		cancel()

		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestCtx)

		started := time.Now()
		_, _, err := asyncTaskWait(c, &relaycommon.RelayInfo{}, "task-id")
		require.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(started), 500*time.Millisecond)
	})

	t.Run("poll interval", func(t *testing.T) {
		service.InitHttpClient()
		polled := make(chan struct{}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case polled <- struct{}{}:
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_status":"RUNNING"}}`))
		}))
		defer server.Close()

		requestCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(requestCtx)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}

		result := make(chan error, 1)
		go func() {
			_, _, err := pollAliTask(c, info, "task-id", 0, time.Hour, 20)
			result <- err
		}()

		select {
		case <-polled:
		case <-time.After(time.Second):
			t.Fatal("first poll did not reach the upstream")
		}
		cancel()

		select {
		case err := <-result:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("polling did not stop after request cancellation")
		}
	})
}

func TestPollAliTaskLimitsConsecutiveNetworkErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	var attempts atomic.Int32
	handlerErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			select {
			case handlerErr <- errors.New("response writer does not support hijacking"):
			default:
			}
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			select {
			case handlerErr <- fmt.Errorf("hijack connection: %w", err):
			default:
			}
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}

	_, _, err := pollAliTask(c, info, "task-id", 0, 0, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.Equal(t, int32(3), attempts.Load())
	select {
	case err := <-handlerErr:
		require.NoError(t, err)
	default:
	}
}
