package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replaceLookupFn 用记录调用的假实现替换后台查询入口，返回已调用 IP 的通道。
// 进入和退出时都清空全局去重表，保证测试可重复运行(-count>1)且互不影响。
func replaceLookupFn(t *testing.T) chan string {
	t.Helper()
	resetIpAutoLookupStore := func() {
		ipAutoLookupStore.Range(func(key, _ interface{}) bool {
			ipAutoLookupStore.Delete(key)
			return true
		})
	}
	resetIpAutoLookupStore()
	calls := make(chan string, 16)
	original := lookupIpInfoFn
	lookupIpInfoFn = func(ip string) (*model.IpInfo, error) {
		calls <- ip
		return &model.IpInfo{Ip: ip}, nil
	}
	t.Cleanup(func() {
		lookupIpInfoFn = original
		resetIpAutoLookupStore()
	})
	return calls
}

func waitLookupCall(t *testing.T, calls chan string, timeout time.Duration) (string, bool) {
	t.Helper()
	select {
	case ip := <-calls:
		return ip, true
	case <-time.After(timeout):
		return "", false
	}
}

// TestScheduleIpInfoLookupSkipsInvalidAndPrivate pins the filter contract: only
// routable public IPs may trigger an external lookup.
func TestScheduleIpInfoLookupSkipsInvalidAndPrivate(t *testing.T) {
	calls := replaceLookupFn(t)
	skipped := []string{
		"",
		"not-an-ip",
		"0.0.0.0",
		"::",
		"127.0.0.1",
		"10.0.0.1",
		"172.16.5.4",
		"192.168.1.100",
		"::1",
		"fe80::1",
		"fc00::1",         // IPv6 ULA
		"255.255.255.255", // IPv4 广播
		"224.0.1.1",       // 非链路本地组播
		"ff05::1",         // IPv6 站点本地组播
	}
	for _, ip := range skipped {
		assert.False(t, ScheduleIpInfoLookup(ip), "should skip %q", ip)
	}
	_, called := waitLookupCall(t, calls, 200*time.Millisecond)
	assert.False(t, called, "invalid/private IPs must not schedule a lookup")
}

// TestScheduleIpInfoLookupTriggersOncePerWindow pins the dedupe contract: a
// public IP triggers exactly one background lookup within the dedupe window.
func TestScheduleIpInfoLookupTriggersOncePerWindow(t *testing.T) {
	calls := replaceLookupFn(t)

	assert.True(t, ScheduleIpInfoLookup("8.8.8.8"))
	ip, called := waitLookupCall(t, calls, 5*time.Second)
	require.True(t, called, "public IP should trigger a background lookup")
	assert.Equal(t, "8.8.8.8", ip)

	// 去重窗口内重复调用（含带空白的等价写法）不再触发。
	assert.False(t, ScheduleIpInfoLookup(" 8.8.8.8 "))
	assert.False(t, ScheduleIpInfoLookup("8.8.8.8"))
	_, called = waitLookupCall(t, calls, 200*time.Millisecond)
	assert.False(t, called, "duplicate IP must not trigger another lookup")
}

// TestScheduleIpInfoLookupRetriggersAfterWindow covers the expiry path: once the
// dedupe entry ages out, the same IP is allowed to trigger again.
func TestScheduleIpInfoLookupRetriggersAfterWindow(t *testing.T) {
	calls := replaceLookupFn(t)
	ip := "9.9.9.9"
	assert.True(t, ScheduleIpInfoLookup(ip))
	_, called := waitLookupCall(t, calls, 5*time.Second)
	require.True(t, called)

	// 把时间戳拨回窗口之外，模拟条目过期。
	ipAutoLookupStore.Store(ip, time.Now().Unix()-int64(ipAutoLookupDedupeWindow.Seconds())-1)
	assert.True(t, ScheduleIpInfoLookup(ip))
	_, called = waitLookupCall(t, calls, 5*time.Second)
	assert.True(t, called, "expired dedupe entry should allow a new trigger")
}
