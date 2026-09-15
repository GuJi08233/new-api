package service

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayClientsBoundResponseHeaderWait(t *testing.T) {
	original := common.RelayResponseHeaderTimeout
	t.Cleanup(func() {
		common.RelayResponseHeaderTimeout = original
		ResetProxyClientCache()
		InitHttpClient()
	})
	for _, testCase := range []struct {
		seconds int
		want    time.Duration
	}{
		{1800, 30 * time.Minute},
		{0, 0},
		{-1, 0},
		{math.MaxInt, time.Duration(math.MaxInt64/int64(time.Second)) * time.Second},
	} {
		common.RelayResponseHeaderTimeout = testCase.seconds
		ResetProxyClientCache()
		InitHttpClient()
		for _, proxyURL := range []string{"", "http://127.0.0.1:1080", "socks5://127.0.0.1:1080"} {
			client, err := GetHttpClientWithProxy(proxyURL)
			require.NoError(t, err)
			transport, ok := client.Transport.(*http.Transport)
			require.True(t, ok)
			assert.Equal(t, testCase.want, transport.ResponseHeaderTimeout, "proxy=%q seconds=%d", proxyURL, testCase.seconds)
		}
	}
}

func TestProxyClientNormalizationAndInvalidation(t *testing.T) {
	ResetProxyClientCache()
	t.Cleanup(ResetProxyClientCache)
	first, err := NewProxyHttpClient("socks5://localhost")
	require.NoError(t, err)
	alias, err := NewProxyHttpClient("socks5://localhost:1080/")
	require.NoError(t, err)
	assert.Same(t, first, alias)
	other, err := NewProxyHttpClient("http://localhost:1081")
	require.NoError(t, err)
	InvalidateProxyClient("socks5://localhost")
	replacement, err := NewProxyHttpClient("socks5://localhost:1080")
	require.NoError(t, err)
	assert.NotSame(t, first, replacement)
	stillOther, err := NewProxyHttpClient("http://localhost:1081")
	require.NoError(t, err)
	assert.Same(t, other, stillOther)
}
