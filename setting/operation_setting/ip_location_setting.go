package operation_setting

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// IpLocationSetting 配置 IP 归属地查询：gitee 密钥与 v4/v6 各自的提供方顺序。
// 查询按顺序逐个尝试，前一个失败(网络错误/非成功响应)时回退到下一个。
type IpLocationSetting struct {
	GiteeApiKey string   `json:"gitee_api_key"`
	Ipv4Order   []string `json:"ipv4_order"`
	Ipv6Order   []string `json:"ipv6_order"`
}

const (
	IpLocationSettingPrefix = "ip_location_setting."

	IpLocationProviderGitee   = "gitee"
	IpLocationProviderIpwhois = "ipwhois"
	IpLocationProviderIp9     = "ip9"
)

// gitee 仅支持 IPv4，因此不出现在 v6 默认顺序里。
var (
	defaultIpv4Order = []string{IpLocationProviderGitee, IpLocationProviderIpwhois, IpLocationProviderIp9}
	defaultIpv6Order = []string{IpLocationProviderIpwhois, IpLocationProviderIp9}
)

func isValidIpLocationProvider(name string) bool {
	switch name {
	case IpLocationProviderGitee, IpLocationProviderIpwhois, IpLocationProviderIp9:
		return true
	}
	return false
}

// ValidateIpLocationOption 校验单个 IP 归属地配置项，非法值在落库前拒绝。
func ValidateIpLocationOption(key string, value string) error {
	if !strings.HasPrefix(key, IpLocationSettingPrefix) {
		return nil
	}

	field := strings.TrimPrefix(key, IpLocationSettingPrefix)
	switch field {
	case "gitee_api_key":
		// 允许为空(未配置时跳过 gitee 提供方)。
		return nil
	case "ipv4_order", "ipv6_order":
		var providers []string
		if err := common.UnmarshalJsonStr(value, &providers); err != nil || providers == nil {
			return fmt.Errorf("IP 查询顺序必须是提供方名称数组")
		}
		seen := map[string]bool{}
		for index, provider := range providers {
			if !isValidIpLocationProvider(provider) {
				return fmt.Errorf("IP 查询顺序第 %d 项无效，可选值为 gitee、ipwhois、ip9", index+1)
			}
			if seen[provider] {
				return fmt.Errorf("IP 查询顺序第 %d 项重复", index+1)
			}
			seen[provider] = true
		}
	default:
		return fmt.Errorf("未知的 IP 归属地配置项: %s", field)
	}
	return nil
}

var ipLocationSetting = IpLocationSetting{
	GiteeApiKey: "",
	Ipv4Order:   append([]string(nil), defaultIpv4Order...),
	Ipv6Order:   append([]string(nil), defaultIpv6Order...),
}

var ipLocationSnapshot atomic.Pointer[IpLocationSetting]

func init() {
	config.GlobalConfig.Register("ip_location_setting", &ipLocationSetting)
	SyncIpLocationSetting()
}

// GetIpLocationSetting 返回只读快照，可被任意请求 goroutine 安全并发读取。
// 顺序为空时回退默认顺序，保证既有部署无需配置即可用。
func GetIpLocationSetting() *IpLocationSetting {
	return ipLocationSnapshot.Load()
}

// ResolvedIpv4Order 返回生效的 IPv4 提供方顺序。
func (s *IpLocationSetting) ResolvedIpv4Order() []string {
	if s == nil || len(s.Ipv4Order) == 0 {
		return defaultIpv4Order
	}
	return s.Ipv4Order
}

// ResolvedIpv6Order 返回生效的 IPv6 提供方顺序。
func (s *IpLocationSetting) ResolvedIpv6Order() []string {
	if s == nil || len(s.Ipv6Order) == 0 {
		return defaultIpv6Order
	}
	return s.Ipv6Order
}

// SyncIpLocationSetting 将暂存配置深拷贝后发布为只读快照。
func SyncIpLocationSetting() {
	snapshot := &IpLocationSetting{}
	data, err := common.Marshal(ipLocationSetting)
	if err != nil || common.Unmarshal(data, snapshot) != nil {
		snapshot = &IpLocationSetting{}
	}
	ipLocationSnapshot.Store(snapshot)
}
