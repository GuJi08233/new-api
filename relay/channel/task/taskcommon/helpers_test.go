package taskcommon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 任务按顶层 model 计价并预扣，metadata 只能补充参数，不能换成别的模型。
// JSON 字段匹配不区分大小写，"Model"、"MODEL" 与 "model" 一样会落到 json:"model" 字段上。
func TestUnmarshalMetadataKeepsPricedModel(t *testing.T) {
	type payload struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	for _, key := range []string{"model", "Model", "MODEL"} {
		t.Run(key, func(t *testing.T) {
			target := payload{Model: "priced-model"}

			err := UnmarshalMetadata(map[string]any{key: "unpriced-model", "prompt": "a cat"}, &target)

			require.NoError(t, err)
			assert.Equal(t, "priced-model", target.Model)
			assert.Equal(t, "a cat", target.Prompt)
		})
	}
}
