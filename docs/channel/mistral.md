# Mistral protocol compatibility

The Mistral channel adapts three client protocols using Mistral's documented
[Chat API](https://docs.mistral.ai/api/endpoint/chat) and
[Responses API](https://docs.mistral.ai/api/endpoint/responses).
Disable request-body pass-through to use these conversions.

| Client endpoint | Upstream endpoint |
| --- | --- |
| `/v1/chat/completions` | `/v1/chat/completions` |
| `/v1/responses` | `/v1/responses` |
| `/v1/messages` | `/v1/responses`, or `/v1/chat/completions` when `stop_sequences` or `top_k` is supplied |

Both streaming and non-streaming requests support text, reasoning, function
calls, and tool-result replay. Claude requests retain text/image ordering,
URL/base64 images and documents, function schemas and `strict`, tool choice,
parallel-tool controls, metadata, and JSON schemas from `output_config.format`
or legacy `output_format`. Multimodal tool results remain structured content,
not JSON-encoded text. Claude `is_error` tool results include an explicit
`Tool execution failed (is_error=true): ` text marker because OpenAI/Mistral
tool outputs do not have an equivalent boolean field.

## Chat parameters

OpenAI `seed` maps to Mistral `random_seed`; it must be an integer representable
as int64. If both names are supplied, their values must agree. Native
`random_seed` preserves int64 precision. Native `min_tokens`, `top_k`,
`repetition_penalty`, `prompt_logprobs`, `top_prompt_logprobs`, `prompt_mode`,
`safe_prompt`, `guardrails`, and `service_tier` are retained, including explicit
zero and false values. `min_tokens` has the same relay safety ceiling as
max-token fields and cannot exceed an explicitly supplied output limit.
Channel-level disabled-field policies still apply.

`max_completion_tokens` takes precedence over `max_tokens`, including an
explicit zero. OpenAI image/file parts are adapted to Mistral image,
document-URL, or file-ID chunks. Historical tool-call IDs are consistently
mapped to Mistral's nine-alphanumeric-character format.

Chat tool definitions currently support function tools, not Mistral-hosted
document libraries, image generation, or connectors. Use `tools` and
`tool_choice`; deprecated `functions` and `function_call` are rejected rather
than silently discarded.

## Responses constraints

Mistral Responses is stateless. Send complete message, reasoning, function-call,
and function-result history in `input`. The relay adds `type: "message"` to
untyped message objects, including assistant `output_text` replay; it does not
guess types for ID-only or otherwise unidentified objects.

Unsupported semantics return HTTP 400 before contacting the upstream:

- `previous_response_id`, `conversation`, prompt templates, and `item_reference`;
- `store: true`, automatic truncation, background execution, nonzero
  `top_logprobs`, moderation, and nonzero frequency/presence penalties;
- unsupported or unidentified input-item types and `/v1/responses/compact`.

Use `store: false` (or omit it), `truncation: "disabled"` (or omit it), and
complete client-managed history. Background execution is also rejected by
shared Responses validation: the relay does not provide asynchronous polling
and settlement.

Supported Responses tools, including namespace definitions, are forwarded
without translating them into Chat tools. OpenAI-only hints such as `include`,
Responses `service_tier`, and reasoning `summary` are filtered; their
provider-specific behavior is not emulated. Forcing Chat-to-Responses
conversion with Chat-only controls returns an explicit error instead of
dropping those controls.

Streamed terminal usage retains cached/reasoning token details, including on
incomplete or failed responses. An upstream usage snapshot with explicit zero
tokens is authoritative rather than replaced with a local estimate.

## Provider and model limits

Protocol compatibility does not add capabilities to a model. Vision, document
input, structured output, tool calling, and accepted reasoning efforts depend
on the selected upstream model. Model validation errors are returned without
silently changing effort, removing a schema, or retrying another endpoint.
For example, the tested `deepseek-v4-1-flash-1017` and `zai-glm-5-3-flash`
models reject `min_tokens`, even when it is zero; the former also rejects
`top_k`. Omit these controls for those models rather than relying on the relay
to silently discard them.

Claude thinking maps to Mistral reasoning effort; an exact Anthropic thinking
token budget or cryptographic thinking signature cannot be reproduced.
Claude-hosted tools, MCP servers, containers, context-management operations,
and opaque/redacted thinking blocks are not transferable and are rejected.
Mistral Chat does not identify which stop sequence matched, so Claude responses
cannot manufacture a reliable `stop_sequence` value. Mistral `model_length`
termination maps to OpenAI `length` and Claude `max_tokens`.
