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

## 渠道级请求透传

同协议中转时，开启渠道的「全部透传」（或同时开启「透传请求体」和「透传请求头」），
关闭「透传时应用模型重定向」，并清除不需要的请求头覆盖。无需打开全局透传开关。

```json
{
    "pass_through_body_enabled": true,
    "pass_through_headers_enabled": true,
    "pass_through_rewrite_model_enabled": false
}
```

请求体保留原始字节，包括 JSON 空白、未知字段、显式 `0`/`false` 和 gzip/br 压缩数据；
不应用参数覆盖或自动补充请求参数。显式开启模型重定向时仍会改写 `model`。
请求头保留重复值、空值和客户端的 `Accept-Encoding`；客户端未发送的
`User-Agent`、`Accept`、`anthropic-version`、组织或品牌头不再自动补充。
上游压缩响应在网关内流式解码，供协议处理和用量统计使用。

以下差异是中转边界，不属于逐字节网络抓包复制：

- 客户端的网关令牌、Cookie 和 `New-Api-User` 不传给上游；使用渠道配置的上游凭据。
- 过滤 `CF-*`、`X-Forwarded-*`、`Forwarded`、`Via` 等代理/CDN 头，以及逐跳头和
  `Connection` 指定的连接级头。无法仅根据头名可靠识别所有代理自定义元数据。
- `Host` 指向上游，`Content-Length`、传输分块、HTTP/WebSocket 握手由出站连接管理；
  头名大小写、头顺序、TLS 和出口 IP 不保证与客户端直连相同。
- 显式请求头覆盖仍优先。SDK 签名渠道（如 AWS、腾讯）仍须生成其协议要求的鉴权/
  签名头，不能把跨协议转换当作原样中转；应选择与客户端请求格式兼容的渠道。

## 渠道请求限额

在渠道编辑的高级设置中配置，所有模型和密钥共用同一渠道限额。`0` 表示不限制，周期间隔除外。

| 渠道 `setting` 字段 | 含义 |
| --- | --- |
| `rate_limit_max_requests` | 每周期承接的请求总数（包括失败、重试和渠道测试）。`rate_limit_period_minutes` 为 `0` 或 `1` 时即为 RPM：按整分钟重置，不另设 `rpm_limit`，旧配置继续生效。 |
| `daily_request_limit` | 每日承接的请求总数。 |
| `daily_success_limit` | 每日成功次数。请求开始时预留名额，失败或取消会释放；跨午夜的请求归到开始当天。异步任务按提交结果计，不按后台轮询结果计。 |
| `concurrency_limit` | 同时处理的请求数。流式请求在结束后释放名额。 |

两个每日限额共用日切时间：优先使用 `daily_request_limit_timezone` 的 IANA 时区（自动处理夏令时），其次是 `daily_request_limit_utc_offset`，都未配置时使用服务器本地时区。普通中转和任务提交遇到限额会尝试其他符合选路规则的渠道；固定渠道不切换，超限返回 `429`。

配置 Redis 时限额由各实例共享；Redis 暂时不可用时，启用限额的渠道拒绝新的尝试，避免突破上限。未配置 Redis 时，限额计数和并发名额仅在当前进程内生效，重启后清零。

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
