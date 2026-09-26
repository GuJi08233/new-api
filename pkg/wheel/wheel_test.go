package wheel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按权重加总：平均每花 1 个游戏币派彩 800/1000 = 0.8 个，玩家长期是输的。这个返还率写在玩家看到的规则里，
// 免费旋转的平均价值也由它决定，改格子时要同步更新这里。
func TestPayback(t *testing.T) {
	paid, total := Payback()
	assert.Equal(t, 1000, total)
	assert.Equal(t, 800, paid)
}

// 权重合计上的每个点都落在唯一的一格里，每格分到的点数正好是它的权重：页面按权重公示的概率就是实际被选中的概率。
func TestSegmentAtMatchesWeights(t *testing.T) {
	_, total := Payback()
	hits := make([]int, len(Segments))
	for point := 0; point < total; point++ {
		hits[SegmentAt(point)]++
	}
	for index, segment := range Segments {
		require.Positive(t, segment.Weight, "segment %d", index)
		require.GreaterOrEqual(t, segment.Multiplier, 0, "segment %d", index)
		assert.Equal(t, segment.Weight, hits[index], "segment %d", index)
	}
}
