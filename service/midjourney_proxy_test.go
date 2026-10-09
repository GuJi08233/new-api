package service

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyRequestsUseChannelProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitHttpClient()
	t.Cleanup(GetHttpClient().CloseIdleConnections)

	for _, disableKeepAlive := range []bool{false, true} {
		t.Run(fmt.Sprintf("disableKeepAlive=%v", disableKeepAlive), func(t *testing.T) {
			ResetProxyClientCache()
			t.Cleanup(ResetProxyClientCache)
			var connections atomic.Int64
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "channel-secret", r.Header.Get("mj-api-secret"))
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(w, `{"code":1,"result":"task-result"}`)
			}))
			origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			origin.Start()
			t.Cleanup(origin.Close)
			proxyAddr, proxyConnections := startCountingSocks5Proxy(t)

			for _, request := range []struct {
				method string
				path   string
				body   string
			}{
				{http.MethodPost, "/mj/submit/imagine", `{"prompt":"cat"}`},
				{http.MethodPost, "/mj/insight-face/swap", `{}`},
				{http.MethodGet, "/mj/task/task-id/image-seed", ""},
			} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
				common.SetContextKey(c, constant.ContextKeyChannelKey, "channel-secret")
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
					Proxy:            "socks5://" + proxyAddr,
					DisableKeepAlive: disableKeepAlive,
				})

				resp, _, err := DoMidjourneyHttpRequest(c, 5*time.Second, origin.URL+request.path)
				require.NoError(t, err)
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, "task-result", resp.Response.Result)
			}
			wantConnections := int64(1)
			if disableKeepAlive {
				wantConnections = 3
			}
			assert.Equal(t, wantConnections, connections.Load())
			assert.Equal(t, wantConnections, atomic.LoadInt64(proxyConnections))
		})
	}
}

func TestMidjourneyInvalidProxyDoesNotFallBackToDirect(t *testing.T) {
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"code":1,"result":"task-result"}`)
	}))
	t.Cleanup(origin.Close)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/mj/task/task-id/image-seed", nil)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{Proxy: "invalid://proxy"})

	resp, _, err := DoMidjourneyHttpRequest(c, 5*time.Second, origin.URL)
	require.Error(t, err)
	assert.Equal(t, "proxy_url_invalid", resp.Response.Description)
	assert.Zero(t, requests.Load())
}
