package service

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newConnectionCountingServer 统计服务端接受的 TCP 连接数。
// 同一个客户端连续发起多次请求时，连接数是否增长即代表是否复用了连接。
func newConnectionCountingServer(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var connections int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt64(&connections, 1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return server, &connections
}

func getAndDrain(t *testing.T, client *http.Client, url string) {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestChannelClientReusesPooledConnectionByDefault(t *testing.T) {
	ResetProxyClientCache()
	InitHttpClient()
	t.Cleanup(func() {
		ResetProxyClientCache()
		InitHttpClient()
	})

	server, connections := newConnectionCountingServer(t)
	client, err := GetChannelHttpClient("", false)
	require.NoError(t, err)
	require.Same(t, GetHttpClient(), client, "未开启该开关时应继续复用默认客户端")

	for i := 0; i < 3; i++ {
		getAndDrain(t, client, server.URL)
	}
	assert.Equal(t, int64(1), atomic.LoadInt64(connections), "默认行为应复用同一条空闲连接")
}

func TestChannelClientDisableKeepAliveOpensConnectionPerRequest(t *testing.T) {
	ResetProxyClientCache()
	InitHttpClient()
	t.Cleanup(func() {
		ResetProxyClientCache()
		InitHttpClient()
	})

	server, connections := newConnectionCountingServer(t)
	client, err := GetChannelHttpClient("", true)
	require.NoError(t, err)
	require.NotSame(t, GetHttpClient(), client, "要求每次新建连接时不能借用默认客户端的长连接池")

	for i := 0; i < 3; i++ {
		getAndDrain(t, client, server.URL)
	}
	assert.Equal(t, int64(3), atomic.LoadInt64(connections), "每次请求都应新建一条连接，代理池才能为每个请求分配新的出口")
}

func TestChannelClientDisableKeepAliveDisablesHTTP2(t *testing.T) {
	ResetProxyClientCache()
	InitHttpClient()
	t.Cleanup(func() {
		ResetProxyClientCache()
		InitHttpClient()
	})

	for _, proxyURL := range []string{"", "http://127.0.0.1:1080", "socks5://127.0.0.1:1080"} {
		client, err := GetChannelHttpClient(proxyURL, true)
		require.NoError(t, err)
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok, "proxy=%q", proxyURL)
		assert.True(t, transport.DisableKeepAlives, "proxy=%q", proxyURL)
		assert.False(t, transport.ForceAttemptHTTP2, "proxy=%q", proxyURL)
		assert.NotNil(t, transport.TLSNextProto, "proxy=%q", proxyURL)

		reused, err := GetChannelHttpClient(proxyURL, false)
		require.NoError(t, err)
		assert.NotSame(t, client, reused, "同一代理地址的两种连接复用策略必须各自缓存，proxy=%q", proxyURL)

		if proxyURL == "" {
			continue
		}
		reusedTransport, ok := reused.Transport.(*http.Transport)
		require.True(t, ok, "proxy=%q", proxyURL)
		assert.False(t, reusedTransport.DisableKeepAlives, "proxy=%q", proxyURL)
		assert.True(t, reusedTransport.ForceAttemptHTTP2, "proxy=%q", proxyURL)
		assert.Nil(t, reusedTransport.TLSNextProto, "proxy=%q", proxyURL)

		again, err := GetChannelHttpClient(proxyURL, true)
		require.NoError(t, err)
		assert.Same(t, client, again, "相同策略应命中缓存，proxy=%q", proxyURL)
	}
}

// TestChannelClientDisableKeepAliveThroughSocks5 覆盖用户实际的代理池场景：
// 入口是 socks5 代理，开启开关后每个请求都必须各自建立到代理的新连接。
func TestChannelClientDisableKeepAliveThroughSocks5(t *testing.T) {
	ResetProxyClientCache()
	InitHttpClient()
	t.Cleanup(func() {
		ResetProxyClientCache()
		InitHttpClient()
	})

	origin, originConnections := newConnectionCountingServer(t)
	proxyAddr, proxyConnections := startCountingSocks5Proxy(t)

	assertProxyConnectionCount(t, "socks5://"+proxyAddr, true, origin.URL, originConnections, proxyConnections)

	// 关闭开关时应复用同一条到代理的连接，服务端只看到一次连接。
	ResetProxyClientCache()
	assertProxyConnectionCount(t, "socks5://"+proxyAddr, false, origin.URL, originConnections, proxyConnections)
}

func assertProxyConnectionCount(t *testing.T, proxyURL string, disableKeepAlive bool, originURL string, originConnections, proxyConnections *int64) {
	t.Helper()
	atomic.StoreInt64(originConnections, 0)
	atomic.StoreInt64(proxyConnections, 0)

	client, err := GetChannelHttpClient(proxyURL, disableKeepAlive)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		getAndDrain(t, client, originURL)
	}

	want := int64(3)
	if !disableKeepAlive {
		want = 1
	}
	assert.Equal(t, want, atomic.LoadInt64(proxyConnections),
		"disableKeepAlive=%v 时到 socks5 代理的连接数", disableKeepAlive)
	assert.Equal(t, want, atomic.LoadInt64(originConnections),
		"disableKeepAlive=%v 时代理到上游的连接数", disableKeepAlive)
}

// startCountingSocks5Proxy 启动一个最小可用的无认证 SOCKS5 代理，
// 统计代理侧接受的连接数并转发流量，以便验证连接是否被复用。
func startCountingSocks5Proxy(t *testing.T) (string, *int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	var connections int64
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			atomic.AddInt64(&connections, 1)
			go serveSocks5Conn(conn)
		}
	}()
	return listener.Addr().String(), &connections
}

func serveSocks5Conn(conn net.Conn) {
	defer conn.Close()

	// 方法协商：只接受「无认证」。
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}

	// 请求：CONNECT + 目标地址。
	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil || request[1] != 1 {
		return
	}
	var host string
	switch request[3] {
	case 1: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return
		}
		host = net.IP(addr).String()
	case 3: // 域名
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		name := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, name); err != nil {
			return
		}
		host = string(name)
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes))))

	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = conn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	go func() { _, _ = io.Copy(upstream, conn) }()
	_, _ = io.Copy(conn, upstream)
}
