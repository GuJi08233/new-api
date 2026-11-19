# 渠道而外设置说明

该配置用于设置一些额外的渠道参数，可以通过 JSON 对象进行配置。主要包含以下两个设置项：

1. force_format
    - 用于标识是否对数据进行强制格式化为 OpenAI 格式
    - 类型为布尔值，设置为 true 时启用强制格式化

2. proxy
    - 用于配置网络代理
    - 类型为字符串，填写代理地址（例如 socks5 协议的代理地址）

3. thinking_to_content
   - 用于标识是否将思考内容`reasoning_content`转换为`<think>`标签拼接到内容中返回
   - 类型为布尔值，设置为 true 时启用思考内容转换

--------------------------------------------------------------

## JSON 格式示例

以下是一个示例配置，启用强制格式化并设置了代理地址：

```json
{
    "force_format": true,
   "thinking_to_content": true,
    "proxy": "socks5://xxxxxxx"
}
```

--------------------------------------------------------------

通过调整上述 JSON 配置中的值，可以灵活控制渠道的额外行为，比如是否进行格式化以及使用特定的网络代理。

## Mistral 协议兼容

Mistral 渠道支持 OpenAI Chat Completions 和 Responses 请求。Chat 请求会转换
`developer` 角色、图片 URL 对象、`max_completion_tokens` 和工具调用 ID；
助手消息中的 `reasoning_content`（或 `reasoning`）会转换为 Mistral 的
`thinking` 内容块，用于多轮对话回传，而不是混入最终答案。

Claude `/v1/messages` 请求自动转换为 Mistral `/v1/responses`，无需额外开启
Chat Completions 转 Responses 策略。流式和非流式结果均返回 Claude 格式，
保留明文思考、文本、工具调用及用量；历史 `thinking` 块会作为 Responses
reasoning item 回传。此路径要求关闭请求体透传。

对于需要思考历史和工具调用的客户端，推荐 Responses 的结构化 reasoning
items；两个端点使用同一个模型时，并不因为协议不同而提高推理能力。
思考强度通过 `reasoning.effort`（Chat 为 `reasoning_effort`）指定，
Claude 的 `output_config.effort` 优先于 `thinking` 的默认映射；
自动转换时，`enabled` 和 `adaptive` 默认使用 `high`，`disabled` 映射为 `none`。
具体可选值仍取决于模型，例如 Mistral Large 4 支持 `low`、`medium`、`high`。

Mistral Responses 是无状态 API：必须在 `input` 中携带历史消息、思考和工具结果。
仅有 `previous_response_id`、`conversation` ID 或 OpenAI 托管 `prompt` 模板时，
网关无法恢复其内容，仍会明确报错，不能通过丢弃这些字段来假装完成转换。
其他提供商的加密思考或签名也不能转换成 Mistral 的明文思考。
参考 [Mistral Responses 文档](https://docs.mistral.ai/inference/conversations/responses)
和[思考历史回传说明](https://docs.mistral.ai/inference/conversations/reasoning)。
