package setting

import (
	"crypto/subtle"
	"strconv"

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
// 返回的地址直接塞进 <img>，无法附带鉴权头——而链接里的任务标识可被直接枚举。
// 因此用签名限定访问：只有从任务查询接口拿到过链接的调用方才持有可用的签名。
//
// 链接标识用任务行主键而不是 mj_id：mj_id 只有普通索引，上游返回 21/22（任务已存在、
// 排队中）时仍会为同一个 mj_id 再插一行，不同用户也可能拿到同一个 mj_id。按 mj_id 签名
// 等于让一份合法签名对所有同名任务通用，持有自己任务链接的用户可以取走别人的图。
func MjForwardImageURL(taskId int) string {
	id := strconv.Itoa(taskId)
	return system_setting.ServerAddress + "/mj/image/" + id +
		"?" + MjForwardImageSignParam + "=" + mjForwardImageSign(id)
}

// VerifyMjForwardImageSign 恒定时间比对转发链接签名，taskId 为链接里的路径段原文。
func VerifyMjForwardImageSign(taskId string, sign string) bool {
	return subtle.ConstantTimeCompare([]byte(sign), []byte(mjForwardImageSign(taskId))) == 1
}

func mjForwardImageSign(taskId string) string {
	// 域分隔串带 -task 后缀：mj_id 本身就是数字串，沿用旧前缀会让按 mj_id 签发的历史链接
	// 直接对同数值的任务行主键生效。
	mac := common.GenerateHMAC("mj-forward-image-task:" + taskId)
	if len(mac) > mjForwardImageSignLength {
		return mac[:mjForwardImageSignLength]
	}
	return mac
}
