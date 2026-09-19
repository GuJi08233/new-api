package setting

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /mj/image 按设计允许匿名取图，签名是它唯一的访问凭证。签名必须绑定到唯一的任务行，
// 否则上游按序生成的任务 ID 可以被直接遍历，取走其他用户生成的图片。
func TestMjForwardImageURLSignIsBoundToTaskRow(t *testing.T) {
	originAddress := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = originAddress })
	system_setting.ServerAddress = "https://example.com"

	forwardURL := MjForwardImageURL(7)
	parsed, err := url.Parse(forwardURL)
	require.NoError(t, err)
	assert.Equal(t, "/mj/image/7", parsed.Path)

	sign := parsed.Query().Get(MjForwardImageSignParam)
	require.NotEmpty(t, sign)

	assert.True(t, VerifyMjForwardImageSign("7", sign))
	assert.False(t, VerifyMjForwardImageSign("8", sign))
	assert.False(t, VerifyMjForwardImageSign("7", ""))
	assert.False(t, VerifyMjForwardImageSign("7", sign[:len(sign)-1]))
}

// 同一个 mj_id 可以对应多行（上游返回 21/22 时会为已存在的任务再插一行，不同用户也
// 可能拿到同一个 mj_id）。签名必须逐行不同，否则持有自己任务链接的用户可以读到别人的行。
func TestMjForwardImageSignDiffersPerTaskRowWithSameMjId(t *testing.T) {
	originAddress := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = originAddress })
	system_setting.ServerAddress = "https://example.com"

	ownSign, err := url.Parse(MjForwardImageURL(11))
	require.NoError(t, err)
	otherSign, err := url.Parse(MjForwardImageURL(12))
	require.NoError(t, err)

	assert.NotEqual(t,
		ownSign.Query().Get(MjForwardImageSignParam),
		otherSign.Query().Get(MjForwardImageSignParam),
	)
	assert.False(t, VerifyMjForwardImageSign("12", ownSign.Query().Get(MjForwardImageSignParam)))
}
