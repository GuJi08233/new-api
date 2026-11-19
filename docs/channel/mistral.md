# Mistral protocol compatibility

The Mistral channel adapts three client protocols using Mistral's documented
[Chat API](https://docs.mistral.ai/api/endpoint/chat),
[Responses API](https://docs.mistral.ai/api/endpoint/responses), and
[Models API](https://docs.mistral.ai/api/endpoint/models).
Disable request-body pass-through to use these conversions.

| Client endpoint | Upstream endpoint |
| --- | --- |
| `/v1/chat/completions` | `/v1/chat/completions` |
| `/v1/responses` | `/v1/responses` |
| `/v1/responses/input_tokens` | `/v1/responses/input_tokens` |
| `/v1/messages` | `/v1/responses`, or `/v1/chat/completions` when `stop_sequences` is supplied |

Both streaming and non-streaming requests support text, reasoning, function
calls, and tool-result replay. Standard OpenAI and Anthropic requests are
adapted permissively: unsupported client fields, server tools, stateful
Responses options, and opaque reasoning blocks are sanitized or omitted rather
than rejected with HTTP 400.

## Chat parameters

- OpenAI `developer` role maps to `system`, and legacy `function` role maps to
  `tool`. Because Mistral Chat schemas disallow `name` on `system`/`user`/
  `assistant` messages, `name` and `tool_call_id` are sent only on `tool`
  messages, and `prefix`/`tool_calls` only on `assistant` messages.
- Legacy `functions` and `function_call` are automatically converted to
  standard `tools` and `tool_choice`; non-function tools are filtered cleanly.
- `max_completion_tokens` takes precedence over `max_tokens` (including `0`).
  `seed` maps to `random_seed` when `random_seed` is unset. `service_tier`
  normalizes `"priority"` to `"auto"`, preserves `"auto"` and `"standard_only"`,
  and omits unrecognized OpenAI values.
- Model-specific unsupported controls are automatically omitted to avoid
  upstream 400 errors (for example, `min_tokens` on `deepseek-v4-*` and
  `zai-glm-5-3*`, and `top_k` / `logprobs` on `deepseek-v4-*`).
- OpenAI image/file parts (including `input_text`, `input_image`, `input_file`)
  are adapted to Mistral `text`, `image_url`, `document_url`, or `file` chunks.
  Historical tool-call IDs are consistently mapped to Mistral's
  nine-alphanumeric-character format.
- Reasoning responses returned as chunk arrays are normalized to standard
  OpenAI `content` + `reasoning_content`; `finish_reason: "model_length"` maps
  to `"length"`.

## Responses compatibility

Mistral Responses is stateless. Standard OpenAI Responses requests are adapted
without rejecting common client defaults:

- Stateful or OpenAI-only parameters (`store`, `truncation`, `background`,
  `previous_response_id`, `conversation`, `prompt`, `top_logprobs`,
  `moderation`, `frequency_penalty`, `presence_penalty`, `include`,
  `service_tier`, `user`, `safety_identifier`, `prompt_cache_retention`,
  `reasoning.summary`) are cleanly omitted.
- Untyped message items in `input` receive `type: "message"`, missing content
  defaults to `""`, `encrypted_content` is stripped from `reasoning` items
  (opaque items without plaintext content/summary are dropped), and unsupported
  input item types (such as `item_reference` or built-in tool calls) are
  skipped safely.
- `tools` keeps Mistral-supported `function` and `namespace` tools while
  filtering out OpenAI built-in tools; `tool_choice` (`allowed_tools` or
  filtered built-in tool selectors) is normalized safely.
- `metadata` values are coerced to strings as required by Mistral Responses.
- Whole-number float timestamps (`created_at`, `completed_at`) returned by
  Mistral are normalized to JSON integers for strict OpenAI SDKs.
- `POST /v1/responses/input_tokens` forwards the documented token-counting
  fields without charging generation quota.

## Claude Messages compatibility

`/v1/messages` converts directly to Mistral `/v1/responses` (recording a single
`Claude Messages -> OpenAI Responses` conversion hop in logs), or directly to
`/v1/chat/completions` when `stop_sequences` is supplied and forced Responses
conversion is disabled.

- Claude text/image/document ordering, URL/base64/text documents, function
  schemas and `strict`, `tool_choice`, `disable_parallel_tool_use`, `metadata`,
  and `output_config.format` / `output_format` (`json_schema` and
  `json_object`) are preserved.
- Anthropic-only features (`context_management`, `container`, `mcp_servers`,
  server tools such as `web_search_*`, `redacted_thinking`, and unknown blocks)
  are ignored safely instead of returning HTTP 400.

## Reasoning effort normalization

Mistral-hosted models accept different subsets of `reasoning_effort` values
(for example, `deepseek-v4-1-flash-1017` rejects `medium`/`minimal`,
`zai-glm-5-3-flash` accepts only `low`/`high`/`max`, `mistral-large-4` accepts
only `low`/`medium`/`high`, and `mistral-medium-3-5` / `mistral-small-2603`
accept only `none`/`high`). Standard effort values from OpenAI or Claude
clients are automatically mapped to the nearest effort accepted by the target
model family.
