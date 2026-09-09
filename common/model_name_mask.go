package common

import "strings"

// MaskModelNameInText 把面向用户的文案里出现的上游模型名替换成客户端请求的模型名，
// 用于隐藏渠道模型重定向的痕迹（错误文案、日志正文、任务失败原因）。
//
// 当请求名本身包含上游名时（例如上游 gpt-4、请求 gpt-4-turbo）直接返回原文：
// 这种前缀关系下做子串替换会把文案里原本正确的请求名改坏（gpt-4-turbo-turbo），
// 宁可少脱敏一次，也不能产出错误的模型名。
func MaskModelNameInText(text string, upstreamModel string, requestModel string) string {
	if text == "" || upstreamModel == "" || requestModel == "" || upstreamModel == requestModel {
		return text
	}
	if strings.Contains(requestModel, upstreamModel) {
		return text
	}
	return strings.ReplaceAll(text, upstreamModel, requestModel)
}
