package setting

import (
	"crypto/subtle"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

var MjNotifyEnabled = false
var MjAccountFilterEnabled = false
var MjModeClearEnabled = false
var MjForwardUrlEnabled = true
var MjActionCheckSuccessEnabled = true

// MjForwardImageSignParam 是转发链接携带签名的查询参数名。
const MjForwardImageSignParam = "sign"

// mjForwardImageSignLength 把 HMAC 截断到 32 个十六进制字符（128 位）：
// 足以让链接不可猜测，又不至于让图片地址长得没法用。
const mjForwardImageSignLength = 32

// MjForwardImageURL 构造 /mj/image 转发链接。该端点按设计允许匿名取图——客户端会把
// 返回的地址直接塞进 <img>，无法附带鉴权头——而任务 ID 由上游按序生成、可被枚举。
// 因此用签名限定访问：只有从任务查询接口拿到过链接的调用方才持有可用的签名。
func MjForwardImageURL(mjId string) string {
	// 任务 ID 来自上游，转义后再拼接，避免其中的 ? 或 # 把签名参数吃掉。
	return system_setting.ServerAddress + "/mj/image/" + url.PathEscape(mjId) +
		"?" + MjForwardImageSignParam + "=" + mjForwardImageSign(mjId)
}

// VerifyMjForwardImageSign 恒定时间比对转发链接签名。
func VerifyMjForwardImageSign(mjId string, sign string) bool {
	return subtle.ConstantTimeCompare([]byte(sign), []byte(mjForwardImageSign(mjId))) == 1
}

func mjForwardImageSign(mjId string) string {
	mac := common.GenerateHMAC("mj-forward-image:" + mjId)
	if len(mac) > mjForwardImageSignLength {
		return mac[:mjForwardImageSignLength]
	}
	return mac
}
