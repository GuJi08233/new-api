package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	walletConnectOfficialRelayURL = "wss://relay.walletconnect.com/"

	// Each bridged session pins two goroutines and two sockets for as long as it
	// stays open, so the proxy needs its own ceiling and liveness checks: without
	// them a handful of idle connections can hold those resources indefinitely.
	walletConnectRelayMaxConnections = 256
	// One wallet session needs a single relay socket; a few more cover
	// reconnects. Without a per-user ceiling one account could take the whole
	// global budget and lock every other payer out.
	walletConnectRelayMaxConnectionsPerUser = 4
	walletConnectRelayMaxMessageSize        = 1 << 20 // 1 MiB, well above any relay frame
	walletConnectRelayIdleTimeout           = 90 * time.Second
	walletConnectRelayPingInterval          = 30 * time.Second
	walletConnectRelayWriteTimeout          = 15 * time.Second
)

var (
	walletConnectRelayConnections     atomic.Int64
	walletConnectRelayUserConnections = struct {
		sync.Mutex
		counts map[int]int
	}{counts: map[int]int{}}
	errWalletConnectProjectMismatch = errors.New("walletconnect projectId does not match the configured project")
)

// WalletConnectRelayProxy proxies WalletConnect v2 Relay WebSocket traffic.
// It is intentionally pinned to the official relay to avoid open-proxy abuse
// and to prevent accidental proxy-to-proxy chains.
func WalletConnectRelayProxy(c *gin.Context) {
	if !setting.EthereumWalletConnectRelayProxyEnabled {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "WalletConnect relay proxy is disabled",
		})
		return
	}

	if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"service": "WalletConnect Relay proxy",
			"target":  walletConnectOfficialRelayURL,
			"usage":   "Use this endpoint as a WebSocket relayUrl from the dApp frontend.",
		})
		return
	}

	upstreamURL, err := buildWalletConnectUpstreamURL(c.Request.URL.RawQuery)
	if errors.Is(err, errWalletConnectProjectMismatch) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "WalletConnect projectId is not allowed on this relay",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid WalletConnect relay query",
		})
		return
	}

	userId := c.GetInt("id")
	walletConnectRelayUserConnections.Lock()
	userBusy := walletConnectRelayUserConnections.counts[userId] >= walletConnectRelayMaxConnectionsPerUser
	if !userBusy {
		walletConnectRelayUserConnections.counts[userId]++
	}
	walletConnectRelayUserConnections.Unlock()
	if userBusy || walletConnectRelayConnections.Add(1) > walletConnectRelayMaxConnections {
		if !userBusy {
			walletConnectRelayConnections.Add(-1)
			releaseWalletConnectRelayUserSlot(userId)
		}
		common.SysLog(fmt.Sprintf("WalletConnect Relay 代理连接数已达上限，拒绝新连接: userId=%d perUserLimit=%t", userId, userBusy))
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "WalletConnect relay proxy is busy",
		})
		return
	}
	defer walletConnectRelayConnections.Add(-1)
	defer releaseWalletConnectRelayUserSlot(userId)

	requestedSubprotocols := websocket.Subprotocols(c.Request)
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 15 * time.Second,
		Subprotocols:     requestedSubprotocols,
	}
	upstream, resp, err := dialer.Dial(upstreamURL, nil)
	if err != nil {
		status := http.StatusBadGateway
		if resp != nil && resp.StatusCode > 0 {
			status = resp.StatusCode
		}
		common.SysLog(fmt.Sprintf("WalletConnect Relay 代理连接上游失败: status=%d query=%s err=%v", status, safeWalletConnectRelayQuery(c.Request.URL.Query()), err))
		c.JSON(status, gin.H{
			"success": false,
			"message": "WalletConnect relay upstream connection failed",
		})
		return
	}
	defer upstream.Close()

	acceptedSubprotocols := requestedSubprotocols
	if upstream.Subprotocol() != "" {
		acceptedSubprotocols = []string{upstream.Subprotocol()}
	}
	// No CheckOrigin override: gorilla's default only accepts handshakes whose
	// Origin matches the request host. The relay is authenticated by the session
	// cookie, so a cross-site page must not be able to ride on it.
	upgrader := websocket.Upgrader{
		Subprotocols: acceptedSubprotocols,
	}
	client, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		common.SysLog(fmt.Sprintf("WalletConnect Relay 代理升级客户端连接失败: %v", err))
		return
	}
	defer client.Close()

	common.SysLog(fmt.Sprintf("WalletConnect Relay 代理连接已建立: query=%s", safeWalletConnectRelayQuery(c.Request.URL.Query())))
	bridgeWalletConnectRelay(client, upstream)
}

func releaseWalletConnectRelayUserSlot(userId int) {
	walletConnectRelayUserConnections.Lock()
	defer walletConnectRelayUserConnections.Unlock()
	if walletConnectRelayUserConnections.counts[userId] <= 1 {
		delete(walletConnectRelayUserConnections.counts, userId)
		return
	}
	walletConnectRelayUserConnections.counts[userId]--
}

// buildWalletConnectUpstreamURL pins the upstream to the official relay and,
// when a project is configured, to that project: the proxy exists for this
// site's checkout, not as a general egress for anyone's WalletConnect app.
func buildWalletConnectUpstreamURL(rawQuery string) (string, error) {
	target, err := url.Parse(walletConnectOfficialRelayURL)
	if err != nil {
		return "", err
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", err
	}
	configuredProjectID := strings.TrimSpace(setting.EthereumWalletConnectProjectID)
	if configuredProjectID != "" && query.Get("projectId") != configuredProjectID {
		return "", errWalletConnectProjectMismatch
	}
	target.RawQuery = rawQuery
	return target.String(), nil
}

func safeWalletConnectRelayQuery(values url.Values) string {
	copyValues := url.Values{}
	for key, items := range values {
		if key == "auth" || key == "symKey" || key == "relay-protocol" {
			copyValues[key] = []string{"[redacted]"}
			continue
		}
		copyValues[key] = append([]string(nil), items...)
	}
	return copyValues.Encode()
}

func bridgeWalletConnectRelay(client, upstream *websocket.Conn) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}

	configureWalletConnectRelayConn(client)
	configureWalletConnectRelayConn(upstream)

	var wg sync.WaitGroup
	wg.Add(2)
	go proxyWalletConnectMessages(client, upstream, closeBoth, &wg)
	go proxyWalletConnectMessages(upstream, client, closeBoth, &wg)

	done := make(chan struct{})
	go keepAliveWalletConnectRelay(done, client, upstream)
	wg.Wait()
	close(done)
}

// configureWalletConnectRelayConn bounds a bridged connection so a peer that
// stops reading or never speaks again cannot hold the socket pair open.
func configureWalletConnectRelayConn(conn *websocket.Conn) {
	conn.SetReadLimit(walletConnectRelayMaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(walletConnectRelayIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(walletConnectRelayIdleTimeout))
	})
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(walletConnectRelayIdleTimeout))
		// WriteControl is the only write method safe to call while the
		// forwarding goroutine may be inside WriteMessage.
		err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(walletConnectRelayWriteTimeout))
		if err == websocket.ErrCloseSent {
			return nil
		}
		return err
	})
}

func keepAliveWalletConnectRelay(done <-chan struct{}, conns ...*websocket.Conn) {
	ticker := time.NewTicker(walletConnectRelayPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			for _, conn := range conns {
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(walletConnectRelayWriteTimeout)); err != nil {
					return
				}
			}
		}
	}
}

func proxyWalletConnectMessages(src, dst *websocket.Conn, closeBoth func(), wg *sync.WaitGroup) {
	defer wg.Done()
	defer closeBoth()
	for {
		messageType, payload, err := src.ReadMessage()
		if err != nil {
			return
		}
		// Traffic in either direction proves the peer is still alive.
		_ = src.SetReadDeadline(time.Now().Add(walletConnectRelayIdleTimeout))
		if err := dst.SetWriteDeadline(time.Now().Add(walletConnectRelayWriteTimeout)); err != nil {
			return
		}
		if err := dst.WriteMessage(messageType, payload); err != nil {
			return
		}
	}
}
