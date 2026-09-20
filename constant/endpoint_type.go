package constant

type EndpointType string

const (
	EndpointTypeOpenAI                EndpointType = "openai"
	EndpointTypeOpenAIResponse        EndpointType = "openai-response"
	EndpointTypeOpenAIResponseCompact EndpointType = "openai-response-compact"
	EndpointTypeAnthropic             EndpointType = "anthropic"
	EndpointTypeGemini                EndpointType = "gemini"
	EndpointTypeJinaRerank            EndpointType = "jina-rerank"
	EndpointTypeImageGeneration       EndpointType = "image-generation"
	EndpointTypeEmbeddings            EndpointType = "embeddings"
	EndpointTypeOpenAIVideo           EndpointType = "openai-video"
	EndpointTypeTypeSafe              EndpointType = "typesafe"
	//EndpointTypeMidjourney     EndpointType = "midjourney-proxy"
	//EndpointTypeSuno           EndpointType = "suno-proxy"
	//EndpointTypeKling          EndpointType = "kling"
	//EndpointTypeJimeng         EndpointType = "jimeng"
)

// TypeSafeRoutePrefix mounts a second, TypeSafe-native copy of the System One
// endpoints. TypeSafe's SDKs send a plain bearer token with no header that
// distinguishes them from an OpenAI client, so the gateway cannot pick their
// response format on the shared /v1 paths the way it does for Anthropic and
// Gemini. Pointing TYPESAFE_BASE_URL at this prefix identifies them by path
// instead: /typesafe/v1/systemone and /typesafe/v1/models.
const TypeSafeRoutePrefix = "/typesafe"
