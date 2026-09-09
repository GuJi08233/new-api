package service

import (
	"bytes"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ModelRewrite 记录本次请求实际生效的模型重定向：Upstream 是发往上游的模型名，
// Request 是客户端请求里写的模型名。
type ModelRewrite struct {
	Upstream string
	Request  string
}

// responseModelPaths 是各家响应体里承载模型名的字段路径。只改这几条固定路径，
// 不递归搜索，避免动到消息内容里恰好叫 model 的字段。
//   - model：OpenAI chat/embedding/responses 非流式与流式、Claude 非流式
//   - message.model：Claude 流式 message_start
//   - response.model：OpenAI Responses 流式事件
//   - modelVersion：Gemini
var responseModelPaths = []string{"model", "message.model", "response.model", "modelVersion"}

// SetModelRewrite 记录生效的模型重定向。两个名字相同或任一为空时清除记录，
// 后续所有改写逻辑都以“上下文里有没有这条记录”作为第一道短路。
func SetModelRewrite(c *gin.Context, upstreamModel string, requestModel string) {
	if c == nil {
		return
	}
	if upstreamModel == "" || requestModel == "" || upstreamModel == requestModel {
		ClearModelRewrite(c)
		return
	}
	common.SetContextKey(c, constant.ContextKeyModelRewrite, ModelRewrite{
		Upstream: upstreamModel,
		Request:  requestModel,
	})
}

// ClearModelRewrite 清除重定向记录。重试换渠道后必须调用，否则上一个渠道的
// 重定向会残留下来，作用到一个并没有重定向的响应上。
func ClearModelRewrite(c *gin.Context) {
	if c == nil || c.Keys == nil {
		return
	}
	delete(c.Keys, string(constant.ContextKeyModelRewrite))
}

func GetModelRewrite(c *gin.Context) (ModelRewrite, bool) {
	if c == nil {
		return ModelRewrite{}, false
	}
	return common.GetContextKeyType[ModelRewrite](c, constant.ContextKeyModelRewrite)
}

// RewriteResponseModelName 把响应体里的上游模型名改回客户端请求的模型名，
// 返回改写后的数据以及是否真的改动过。仅在渠道发生了模型重定向、且全局开启
// “响应体模型名回写”时生效；其余情况原样返回，调用方可以无条件套用。
func RewriteResponseModelName(c *gin.Context, data []byte) ([]byte, bool) {
	rewrite, ok := responseModelRewrite(c)
	if !ok {
		return data, false
	}
	// 绝大多数流式分片不含模型名，字节包含判断先把它们挡掉。
	if !bytes.Contains(data, []byte(rewrite.Upstream)) {
		return data, false
	}
	return rewriteResponseModelPaths(data, rewrite.Request)
}

// RewriteResponseModelNameString 是 RewriteResponseModelName 的字符串形态，供 SSE 出口使用。
func RewriteResponseModelNameString(c *gin.Context, data string) string {
	rewrite, ok := responseModelRewrite(c)
	if !ok || !strings.Contains(data, rewrite.Upstream) {
		return data
	}
	rewritten, changed := rewriteResponseModelPaths([]byte(data), rewrite.Request)
	if !changed {
		return data
	}
	return string(rewritten)
}

// upstreamModelHeadersLower 是上游用来标注实际服务模型的响应头。OpenAI 会返回
// openai-model，其值可能带上游自己的版本后缀（gpt-4o-2024-08-06），所以这些头
// 整体改写成客户端请求的模型名，而不是做子串替换。
var upstreamModelHeadersLower = map[string]struct{}{
	"openai-model": {},
}

// rewriteUpstreamModelHeaderValues 在响应体模型名回写生效时，同步抹掉响应头里的
// 上游模型名——body 改了而响应头还留着真名等于没改。values 必须是调用方自己的
// 切片，函数原地改写。除已知的模型标识头外，只改动值恰好等于上游模型名的头。
func rewriteUpstreamModelHeaderValues(c *gin.Context, headerNameLower string, values []string) {
	rewrite, ok := responseModelRewrite(c)
	if !ok {
		return
	}
	_, isModelHeader := upstreamModelHeadersLower[headerNameLower]
	for i, value := range values {
		if isModelHeader || value == rewrite.Upstream {
			values[i] = rewrite.Request
		}
	}
}

// MaskUpstreamModelInText 把面向用户的文案（错误消息等）里出现的上游模型名换成
// 客户端请求的模型名。仅在开启“对普通用户隐藏模型重定向信息”时生效；后端日志与
// 管理员可见的错误日志记录在此之前，保留原文。
func MaskUpstreamModelInText(c *gin.Context, text string) string {
	if text == "" || !common.IsHideModelMappingForUserEnabled() {
		return text
	}
	rewrite, ok := GetModelRewrite(c)
	if !ok {
		return text
	}
	return strings.ReplaceAll(text, rewrite.Upstream, rewrite.Request)
}

func responseModelRewrite(c *gin.Context) (ModelRewrite, bool) {
	if !model_setting.GetGlobalSettings().RewriteResponseModelEnabled {
		return ModelRewrite{}, false
	}
	return GetModelRewrite(c)
}

// rewriteResponseModelPaths 覆盖 responseModelPaths 中已存在字符串值的路径，
// 不存在的路径保持原样（不新增字段）。非 JSON 对象（二进制载荷、SSE 的 [DONE]）直接跳过。
func rewriteResponseModelPaths(data []byte, requestModel string) ([]byte, bool) {
	if !isJsonObject(data) {
		return data, false
	}
	rewritten := false
	for _, path := range responseModelPaths {
		if gjson.GetBytes(data, path).Type != gjson.String {
			continue
		}
		updated, err := sjson.SetBytes(data, path, requestModel)
		if err != nil {
			continue
		}
		data = updated
		rewritten = true
	}
	return data, rewritten
}

func isJsonObject(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}
