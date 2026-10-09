package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

// proxyClientKey 唯一标识一个缓存的出站客户端：规范化后的代理地址 + 连接复用策略。
// 同一地址的「复用连接」与「每次新建连接」是两套不同的连接池，必须分开缓存。
type proxyClientKey struct {
	proxyURL         string
	disableKeepAlive bool
}

var (
	httpClient              *http.Client
	ssrfProtectedHTTPClient *http.Client
	proxyClientLock         sync.Mutex
	proxyClients            = make(map[proxyClientKey]*http.Client)
)

// ProxyClientOptions 描述一个渠道级出站代理客户端的可变行为。
type ProxyClientOptions struct {
	// DisableKeepAlive 让每个请求独占一条新建连接，请求结束后立即断开。
	// 代理池按连接分配出口 IP 时，不开启该选项会让同一出口被连续复用。
	DisableKeepAlive bool
}

// applyKeepAliveSetting 按选项配置连接复用。关闭 keep-alive 时同时禁用 HTTP/2：
// HTTP/2 会把同一时刻的并发请求多路复用到同一条 TCP 连接上，那样多个请求仍共用一个
// 代理出口 IP，与「每个请求独占一个新连接」的目标冲突。
func applyKeepAliveSetting(transport *http.Transport, options ProxyClientOptions) {
	if !options.DisableKeepAlive {
		return
	}
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := validateURLWithCurrentFetchSetting(urlStr, true); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func checkProtectedFetchRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := ValidateSSRFProtectedFetchURL(urlStr); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func validateURLWithCurrentFetchSetting(urlStr string, applyDomainIPFilter bool) error {
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, applyDomainIPFilter && fetchSetting.ApplyIPFilterForDomain)
}

func ValidateSSRFProtectedFetchURL(urlStr string) error {
	return validateURLWithCurrentFetchSetting(urlStr, true)
}

// relayDialer 统一出站建连参数。RELAY_CONNECT_TIMEOUT 只约束 TCP 建连阶段：
// 上游 SYN 被丢弃（黑洞）时若不设超时，请求会挂满内核重传周期（Linux 默认约 127 秒）
// 才失败，期间无法换渠道重试。0 表示保持不限制的旧行为。
func relayDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   time.Duration(common.RelayConnectTimeout) * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

func relayTLSHandshakeTimeout() time.Duration {
	if common.RelayConnectTimeout <= 0 {
		return 0
	}
	return 10 * time.Second
}

func relayResponseHeaderTimeout() time.Duration {
	seconds := common.RelayResponseHeaderTimeout
	if seconds <= 0 {
		return 0
	}
	// 秒数转换前限幅，避免溢出成极短的正超时或负数。
	if maxSeconds := int(math.MaxInt64 / int64(time.Second)); seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func InitHttpClient() {
	transport := &http.Transport{
		DialContext:           relayDialer().DialContext,
		MaxIdleConns:          common.RelayMaxIdleConns,
		MaxIdleConnsPerHost:   common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(common.RelayIdleConnTimeout) * time.Second,
		TLSHandshakeTimeout:   relayTLSHandshakeTimeout(),
		ResponseHeaderTimeout: relayResponseHeaderTimeout(),
		ForceAttemptHTTP2:     true,
		Proxy:                 http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	if common.RelayTimeout == 0 {
		httpClient = &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Transport:     transport,
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}
	ssrfProtectedHTTPClient = newProtectedFetchHTTPClient()
}

// GetHttpClient returns the general outbound client used by relay/provider
// integrations. Do not attach the SSRF-protected dialer here: provider base URLs
// are root/operator-managed deployment targets, not arbitrary user-controlled
// input, and may legitimately point at private networks, private-link endpoints,
// self-hosted services, or local proxies. Code paths that fetch arbitrary
// user-controlled URLs must use GetSSRFProtectedHTTPClient or
// ValidateSSRFProtectedFetchURL instead.
func GetHttpClient() *http.Client {
	return httpClient
}

// GetSSRFProtectedHTTPClient 返回带拨号时 SSRF 校验的客户端。
// ssrfProtectedHTTPClient 由 InitHttpClient 在启动时初始化，运行期只读。
func GetSSRFProtectedHTTPClient() *http.Client {
	if fetchSetting := system_setting.GetFetchSetting(); fetchSetting != nil && !fetchSetting.EnableSSRFProtection {
		return GetHttpClient()
	}
	return ssrfProtectedHTTPClient
}

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	return NewProxyHttpClient(proxyURL)
}

// GetChannelHttpClient 按渠道设置返回出站客户端。代理地址为空时回退到默认客户端，
// 除非该渠道要求每个请求独占新连接：那样必须有独立的长连接池，不能借用默认客户端。
func GetChannelHttpClient(proxyURL string, disableKeepAlive bool) (*http.Client, error) {
	return NewProxyHttpClientOptions(proxyURL, ProxyClientOptions{DisableKeepAlive: disableKeepAlive})
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		client.CloseIdleConnections()
	}
	proxyClients = make(map[proxyClientKey]*http.Client)
}

// InvalidateProxyClient 清理不再使用的代理凭据和空闲连接，其他代理保持复用。
// 同一代理地址下的两种连接复用策略都会被清理；空地址对应「无代理但每次新建连接」的客户端。
func InvalidateProxyClient(proxyURL string) {
	normalizedProxyURL := ""
	parsed, _, err := common.ParseProxyURLRuntime(proxyURL)
	if err != nil {
		return
	}
	if parsed != nil {
		normalizedProxyURL = parsed.String()
	}
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for key, client := range proxyClients {
		if key.proxyURL != normalizedProxyURL {
			continue
		}
		client.CloseIdleConnections()
		delete(proxyClients, key)
	}
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	return NewProxyHttpClientOptions(proxyURL, ProxyClientOptions{})
}

// NewProxyHttpClientOptions 创建支持代理的 HTTP 客户端，并应用连接复用策略。
func NewProxyHttpClientOptions(proxyURL string, options ProxyClientOptions) (*http.Client, error) {
	if proxyURL == "" && !options.DisableKeepAlive {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	parsedURL, _, err := common.ParseProxyURLRuntime(proxyURL)
	if err != nil {
		return nil, err
	}
	if parsedURL == nil && !options.DisableKeepAlive {
		return GetHttpClient(), nil
	}
	cacheKey := proxyClientKey{disableKeepAlive: options.DisableKeepAlive}
	if parsedURL != nil {
		cacheKey.proxyURL = parsedURL.String()
	}
	// 同一个规范化地址 + 同一套复用策略只创建一个客户端，失效操作也使用同一把锁。
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	if client, ok := proxyClients[cacheKey]; ok {
		return client, nil
	}

	transport, err := newProxyTransport(parsedURL, options)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	proxyClients[cacheKey] = client
	return client, nil
}

// newProxyTransport 按代理协议构造出站 transport。各协议共享同一套连接池、
// 超时与连接复用配置，只有拨号与代理握手方式不同。parsedURL 为 nil 表示不使用
// 渠道代理，此时与默认客户端一致地跟随 HTTP_PROXY 等环境变量。
func newProxyTransport(parsedURL *url.URL, options ProxyClientOptions) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:          common.RelayMaxIdleConns,
		MaxIdleConnsPerHost:   common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:       time.Duration(common.RelayIdleConnTimeout) * time.Second,
		TLSHandshakeTimeout:   relayTLSHandshakeTimeout(),
		ResponseHeaderTimeout: relayResponseHeaderTimeout(),
		ForceAttemptHTTP2:     true,
	}

	switch {
	case parsedURL == nil:
		transport.DialContext = relayDialer().DialContext
		transport.Proxy = http.ProxyFromEnvironment

	case parsedURL.Scheme == "http" || parsedURL.Scheme == "https":
		transport.DialContext = relayDialer().DialContext
		transport.Proxy = http.ProxyURL(parsedURL)

	case parsedURL.Scheme == "socks5" || parsedURL.Scheme == "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, relayDialer())
		if err != nil {
			return nil, err
		}
		// x/net 的 SOCKS5 拨号器实现了 ContextDialer；走 DialContext 才能在
		// 请求取消时中断代理建连与握手，而不是无限等待
		contextDialer, _ := dialer.(proxy.ContextDialer)

		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if contextDialer != nil {
				return contextDialer.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}

	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	applyKeepAliveSetting(transport, options)

	return transport, nil
}
