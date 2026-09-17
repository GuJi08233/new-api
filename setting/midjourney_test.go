package setting

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /mj/image 按设计允许匿名取图，签名是它唯一的访问凭证。签名必须逐任务绑定，
// 否则上游按序生成的任务 ID 可以被直接遍历，取走其他用户生成的图片。
func TestMjForwardImageURLSignIsBoundToTask(t *testing.T) {
	originAddress := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = originAddress })
	system_setting.ServerAddress = "https://example.com"

	forwardURL := MjForwardImageURL("task-1")
	parsed, err := url.Parse(forwardURL)
	require.NoError(t, err)
	assert.Equal(t, "/mj/image/task-1", parsed.Path)

	sign := parsed.Query().Get(MjForwardImageSignParam)
	require.NotEmpty(t, sign)

	assert.True(t, VerifyMjForwardImageSign("task-1", sign))
	assert.False(t, VerifyMjForwardImageSign("task-2", sign))
	assert.False(t, VerifyMjForwardImageSign("task-1", ""))
	assert.False(t, VerifyMjForwardImageSign("task-1", sign[:len(sign)-1]))
}
