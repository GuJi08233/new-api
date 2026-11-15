# Decisions 原生端点

Gemini Decision 1 和 GPT-6 Luna Decisions 使用各自独立的请求格式。配置现有的 Gemini 或 OpenAI 渠道，在渠道中启用对应模型即可；模型端点模板和渠道测试菜单提供了两个 Decisions 端点。

| 提供商 | 网关端点 | 端点类型 | 模型 | 网关鉴权 |
| --- | --- | --- | --- | --- |
| Gemini | `POST /v1beta/decisions` | `gemini-decisions` | `gemini-decision-1` | `x-goog-api-key: <网关令牌>` |
| OpenAI | `POST /v1/decisions` | `openai-decisions` | `gpt-6-luna` 或 `gpt-6-luna-decisions` | `Authorization: Bearer <网关令牌>` |

上游密钥配置在渠道中。Gemini 上游请求使用 `x-goog-api-key`，OpenAI 上游请求使用 Bearer。两个端点均为非流式接口。

`gpt-6-luna-decisions` 是网关的专用别名，会自动映射为上游的 `gpt-6-luna`，同时保留别名用于模型定价、端点配置和消费日志。透传模式也会恢复这个官方模型名并保留其余请求字段。使用 `gpt-6-luna` 时，在渠道测试中明确选择 OpenAI Decisions；该模型同时支持普通文本生成接口。

## Gemini 请求示例

```json
{
  "model": "gemini-decision-1",
  "input": "用户反馈订单中的手机屏幕已经破损。",
  "questions": {
    "damaged": {
      "type": "noul",
      "instructions": "输入是否提到了商品损坏？"
    },
    "queue": {
      "type": "choice",
      "instructions": "应该交给哪个客服队列？",
      "criteria": {
        "repair": "处理破损商品的维修或换货。",
        "other": "处理其他咨询。"
      }
    }
  }
}
```

`questions` 是对象，包含 1–1000 题；题目 key 不进入模型。题型为 `noul`、`choice`、`score`。`choice.criteria` 是 2–255 个选项的对象；`score.criteria` 是从低到高排列的 2–255 个等级描述。`instructions` 接受文本字符串或文本内容块。

`input` 保留 Gemini 原生的文本、图片、音频、视频、PDF 内容块；内联媒体总量限制为 100 MB，单个 PDF 限制为 50 MB。网关已有的请求体大小设置仍适用。Files API URI 由上游读取，网关本地媒体估算不下载这些文件。

## OpenAI 请求示例

```json
{
  "model": "gpt-6-luna-decisions",
  "input": "用户反馈订单中的手机屏幕已经破损。",
  "questions": [
    {
      "type": "predicate",
      "name": "damaged",
      "instructions": "输入是否提到了商品损坏？"
    },
    {
      "type": "score",
      "name": "severity",
      "instructions": "按影响程度评价这个问题。",
      "levels": [
        { "label": "轻微", "description": "不影响正常使用。" },
        { "label": "严重", "description": "无法正常使用商品。" }
      ]
    }
  ]
}
```

`questions` 是数组，包含 1–400 题，答案保持提问顺序。题型为 `predicate`、`choice`、`score`。`choice.choices` 支持 2–255 个不同的字符串或布尔值；`score.levels` 支持 2–10 级。`name` 可省略。

共享输入和题目说明支持原生文本、图片和 `input_json`；共享输入也支持对话、函数调用及自定义工具调用历史。文件、音频和 PDF 内容块不适用于这个端点。`prompt_cache_key`、`prompt_cache_options` 和内容块中的缓存控制字段会保留。

响应保持上游原生结构。客户端读取 OpenAI 答案前应检查 `type`：单题可能返回 `refusal`，其他题仍可正常回答；`score` 可以是小数。网关不重算或钳制提供商返回的概率、分数与选项分布。

## 计费与验证

两家的基础输入价格均为 $0.10/百万 token，Decisions 输出为 0 token。默认输入倍率为 `0.05`；专用 Decisions 模型的输出倍率为 `0`。OpenAI 缓存读取基础价为 $0.01/百万 token，缓存写入基础价为 $0.125/百万 token，对应读取倍率 `0.1`、写入倍率 `1.25`。已有站点或分组配置可以覆盖默认定价；地区和长上下文附加价可通过现有计费表达式配置。

Gemini 用量读取 `usage.total_input_tokens`；OpenAI 用量读取 `usage.input_tokens` 和 `usage.input_tokens_details.cached_tokens` / `cache_write_tokens`。缓存计数已经包含在输入总量中，由现有结算逻辑分开计价。缺失用量时回退到请求估算；异常超大用量通过统一的 quota 饱和转换处理，并保留管理员审计标记。

两个端点都复用既有的令牌鉴权、模型分组与路由、端点保护、预扣费、结算及消费日志。问题与选项数量在预扣费之前校验，参数覆盖后的请求会再次校验。阶梯预扣费的输出估算固定为 0。上下文窗口、文件实际内容及提供商特有的细节限制由上游执行；本地 token 估算用于预扣费，最终按上游用量结算。

本地回归测试使用可丢弃的 SQLite 数据库和 HTTP 模拟上游，不需要真实提供商密钥。

官方参考（2026-11-16 核对）：[Gemini Decisions API](https://ai.google.dev/gemini-api/docs/decisions-api)、[Gemini Decision 1](https://ai.google.dev/gemini-api/docs/models/gemini-decision-1)、[OpenAI Decisions 指南](https://developers.openai.com/api/docs/guides/decisions)、[OpenAI Decisions API reference](https://developers.openai.com/api/reference/resources/decisions/methods/create)、[GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)。
