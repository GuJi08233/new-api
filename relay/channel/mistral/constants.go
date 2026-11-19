package mistral

import "strings"

var ModelList = []string{
	"mistral-large-latest",
	"mistral-large-4",
	"mistral-medium-latest",
	"mistral-medium-3-5",
	"mistral-small-latest",
	"mistral-small-2603",
	"devstral-latest",
	"codestral-latest",
	"ministral-14b-latest",
	"ministral-8b-latest",
	"ministral-3b-latest",
	"deepseek-v4-1-flash-1017",
	"zai-glm-5-3-flash",
	"zai-glm-5-3",
	"zai-glm-5-2",
	"mistral-embed",
	"mistral-moderation-latest",
	"open-mistral-7b",
	"open-mixtral-8x7b",
}

var ChannelName = "mistral"

func IsModerationModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "mistral-moderation-")
}
