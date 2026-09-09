package common

import "strings"

// MaskModelNameInText 把面向用户的文案里出现的上游模型名替换成客户端请求的模型名，
// 用于隐藏渠道模型重定向的痕迹（错误文案、日志正文、任务失败原因）。
//
// 只替换独立出现的完整模型名：命中位置前后的字符都不能是模型名字符，否则 gpt-4
// 会命中 gpt-4o、gpt-4-turbo 里的片段，把文案里原本正确的名字改坏（gpt-4-turbo-turbo）。
func MaskModelNameInText(text string, upstreamModel string, requestModel string) string {
	if text == "" || upstreamModel == "" || requestModel == "" || upstreamModel == requestModel {
		return text
	}
	var masked strings.Builder
	replaced := false
	cursor := 0
	for {
		offset := strings.Index(text[cursor:], upstreamModel)
		if offset < 0 {
			break
		}
		start := cursor + offset
		end := start + len(upstreamModel)
		standalone := start == 0 || !isModelNameByte(text[start-1])
		if standalone && end < len(text) && isModelNameByte(text[end]) {
			// 紧跟的模型名字符意味着命中的只是更长名字的前缀（gpt-4 之于 gpt-4o）。
			// 例外是句末的 . 或 :，后面不再有模型名字符时它属于文案标点而不是名字。
			next := text[end]
			standalone = (next == '.' || next == ':') && (end+1 >= len(text) || !isModelNameByte(text[end+1]))
		}
		if standalone {
			masked.WriteString(text[cursor:start])
			masked.WriteString(requestModel)
			replaced = true
		} else {
			masked.WriteString(text[cursor:end])
		}
		cursor = end
	}
	if !replaced {
		return text
	}
	masked.WriteString(text[cursor:])
	return masked.String()
}

// isModelNameByte 判断一个字节是否可能属于模型名：字母、数字，以及模型名里常见的
// 连接符（gpt-4o、claude_3、gemini-2.5、claude-3-5-sonnet@20240620、llama3:8b）。
// 斜杠不算：Gemini 的报错写成 models/gemini-2.5-pro，斜杠后面才是要脱敏的名字。
// 多字节字符（如中文）不属于模型名，天然构成边界。
func isModelNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '-', '_', '.', ':', '@':
		return true
	}
	return false
}
