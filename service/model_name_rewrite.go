package service

import (
	"bytes"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ModelRewrite 记录本次请求实际生效的模型重定向。
type ModelRewrite struct {
	// Upstream 是 ModelMappedHelper 映射出的上游模型名（映射时刻的快照）。
	Upstream string
	// Request 是客户端请求里写的模型名。
	Request string
	// info 指向本次请求的 RelayInfo。适配器在映射之后还会继续归一化
	// info.UpstreamModelName（去 -thinking/-nothinking/推理力度后缀、Claude 用
	// message_start 覆盖等），需要"实际发往上游的名字"时以它的实时值为准。
	info *relaycommon.RelayInfo
}

// UpstreamModelName 返回实际发往上游的模型名：优先 RelayInfo 的实时值，退回映射快照。
func (r ModelRewrite) UpstreamModelName() string {
	if r.info != nil && r.info.ChannelMeta != nil && r.info.UpstreamModelName != "" {
		return r.info.UpstreamModelName
	}
	return r.Upstream
}

// upstreamModelNames 返回脱敏时要处理的全部上游名。快照与实时值可能不同，两者都
// 可能出现在上游报错或网关自己的错误文案里。
func (r ModelRewrite) upstreamModelNames() []string {
	live := r.UpstreamModelName()
	if live == r.Upstream {
		return []string{r.Upstream}
	}
	return []string{live, r.Upstream}
}

// responseModelPaths 是各家响应体里承载模型名的字段路径。只改这几条固定路径，
// 不递归搜索，避免动到消息内容里恰好叫 model 的字段。
//   - model：OpenAI chat/embedding/responses 非流式与流式、Claude 非流式
//   - message.model：Claude 流式 message_start
//   - response.model：OpenAI Responses 流式事件
//   - modelVersion：Gemini
var responseModelPaths = []string{"model", "message.model", "response.model", "modelVersion"}

// SetModelRewrite 记录生效的模型重定向。两个名字相同或任一为空时视为没有重定向，
// 后续所有改写逻辑都以“上下文里有没有这条记录”作为第一道短路。
func SetModelRewrite(c *gin.Context, info *relaycommon.RelayInfo, upstreamModel string, requestModel string) {
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
		info:     info,
	})
}

// ClearModelRewrite 清除重定向记录。重试换渠道后必须调用，否则上一个渠道的
// 重定向会残留下来，作用到一个并没有重定向的响应上。写入零值而不是直接删
// c.Keys，让写操作走 gin 自己的锁。
func ClearModelRewrite(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyModelRewrite, ModelRewrite{})
}

func GetModelRewrite(c *gin.Context) (ModelRewrite, bool) {
	if c == nil {
		return ModelRewrite{}, false
	}
	rewrite, ok := common.GetContextKeyType[ModelRewrite](c, constant.ContextKeyModelRewrite)
	if !ok || rewrite.Upstream == "" {
		return ModelRewrite{}, false
	}
	return rewrite, true
}

// modelKeyMarker 是四条改写路径共有的键名片段，用来在真正解析前挡掉不含模型名的
// 分片（Claude 的 content_block_delta、OpenAI 的纯 usage 帧等）。
//
// 这里刻意不按上游模型名过滤：适配器在 ModelMappedHelper 之后还会继续归一化
// info.UpstreamModelName，上游返回的模型名也常带自己的版本后缀，按快照值匹配会让
// 回写在这些场景静默失效。四条固定路径本身已经限制了改写范围。
var modelKeyMarker = []byte("model")

// RewriteResponseModelName 把响应体里的模型名改成客户端请求的模型名，返回改写后的
// 数据以及是否真的改动过。仅在渠道发生了模型重定向、且全局开启“响应体模型名回写”
// 时生效；其余情况原样返回，调用方可以无条件套用。
func RewriteResponseModelName(c *gin.Context, data []byte) ([]byte, bool) {
	rewrite, ok := responseModelRewrite(c)
	if !ok || !bytes.Contains(data, modelKeyMarker) {
		return data, false
	}
	return rewriteResponseModelPaths(data, rewrite.Request)
}

// ResponseModelName 返回该写进响应的模型名：回写生效时是客户端请求的模型名，
// 否则原样返回传入的上游模型名。供自己构造响应结构体、不经过字节改写出口的
// 适配器使用（例如 AWS Nova 直接 c.JSON 输出）。
func ResponseModelName(c *gin.Context, upstreamModelName string) string {
	if rewrite, ok := responseModelRewrite(c); ok {
		return rewrite.Request
	}
	return upstreamModelName
}

// RewriteResponseModelNameString 是 RewriteResponseModelName 的字符串形态，供 SSE 出口使用。
// 输入以只读视图传入不做复制；gjson/sjson 不会改写输入，只有真正改动时才分配新的输出。
func RewriteResponseModelNameString(c *gin.Context, data string) string {
	rewritten, changed := RewriteResponseModelName(c, common.StringToByteSlice(data))
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
// 切片，函数原地改写。除已知的模型标识头外，只改动值恰好等于上游模型名（映射快照
// 或归一化后的实时值）的头。
func rewriteUpstreamModelHeaderValues(rewrite ModelRewrite, headerNameLower string, values []string) {
	_, isModelHeader := upstreamModelHeadersLower[headerNameLower]
	upstreamModelNames := rewrite.upstreamModelNames()
	for i, value := range values {
		if isModelHeader || slices.Contains(upstreamModelNames, value) {
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
	for _, upstreamModel := range rewrite.upstreamModelNames() {
		text = common.MaskModelNameInText(text, upstreamModel, rewrite.Request)
	}
	return text
}

// HideTaskModelMapping 抹掉用户侧任务里的模型重定向痕迹：properties 中的上游模型名、
// 上游原始提交响应（Data）里的模型名，以及失败原因里出现的同一名字。管理端查询不调用它。
func HideTaskModelMapping(taskDto *dto.TaskDto, task *model.Task) {
	if taskDto == nil || task == nil || !common.IsHideModelMappingForUserEnabled() {
		return
	}
	properties := task.Properties
	upstreamModelName := properties.UpstreamModelName
	properties.UpstreamModelName = ""
	taskDto.Properties = properties
	if upstreamModelName == "" || properties.OriginModelName == "" {
		return
	}
	taskDto.FailReason = common.MaskModelNameInText(taskDto.FailReason, upstreamModelName, properties.OriginModelName)
	taskDto.Data = HideModelMappingInTaskBody(task, taskDto.Data)
}

// HideModelMappingInTaskBody 把用户侧任务响应体里的模型名换成用户请求的模型名。body
// 是上游提交接口的原始响应，或按 OpenAI 视频格式转换后的结果，模型名走与响应体回写
// 相同的四条固定路径。开关关闭、任务没有重定向记录或正文没有改动时原样返回。
func HideModelMappingInTaskBody(task *model.Task, body []byte) []byte {
	if task == nil || len(body) == 0 || !common.IsHideModelMappingForUserEnabled() {
		return body
	}
	properties := task.Properties
	if properties.UpstreamModelName == "" || properties.OriginModelName == "" || properties.UpstreamModelName == properties.OriginModelName {
		return body
	}
	rewritten, changed := rewriteResponseModelPaths(body, properties.OriginModelName)
	if !changed {
		return body
	}
	return rewritten
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
	if common.GetJsonType(data) != "object" {
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
