package service

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const ipAutoLookupDedupeWindow = 10 * time.Minute

var (
	// ipAutoLookupStore 记录最近触发过自动查询的 IP 及触发时间(unix 秒)。
	ipAutoLookupStore   sync.Map
	ipAutoLookupCleanup sync.Once
)

// lookupIpInfoFn 是后台自动查询的执行入口，测试中可替换。
var lookupIpInfoFn = LookupIpInfo

func startIpAutoLookupCleanup() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Hour)
			now := time.Now().Unix()
			ipAutoLookupStore.Range(func(key, value interface{}) bool {
				if last, ok := value.(int64); ok {
					if now-last >= int64(ipAutoLookupDedupeWindow.Seconds()) {
						ipAutoLookupStore.Delete(key)
					}
				}
				return true
			})
		}
	})
}

// ScheduleIpInfoLookup 为 relay 请求异步预取客户端 IP 的归属地并写入 ip_infos 缓存表，
// 之后日志/IP 标签展示无需再等待外部接口查询。仅对公网 IP 生效，同一 IP 在去重
// 窗口内只触发一次。返回是否真正触发了一次后台查询。
func ScheduleIpInfoLookup(ip string) bool {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsUnspecified() || parsed.IsPrivate() ||
		parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast() {
		return false
	}
	ip = parsed.String()

	now := time.Now().Unix()
	if last, loaded := ipAutoLookupStore.LoadOrStore(ip, now); loaded {
		if now-last.(int64) < int64(ipAutoLookupDedupeWindow.Seconds()) {
			return false
		}
		// 条目已过期，更新时间戳后重新触发。
		ipAutoLookupStore.Store(ip, now)
	}

	ipAutoLookupCleanup.Do(startIpAutoLookupCleanup)
	gopool.Go(func() {
		if _, err := lookupIpInfoFn(ip); err != nil {
			common.SysLog("auto ip info lookup failed for " + ip + ": " + err.Error())
		}
	})
	return true
}
