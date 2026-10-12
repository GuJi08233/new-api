package g2048

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenGame struct {
	Name           string `json:"name"`
	Seed           uint32 `json:"seed"`
	Moves          string `json:"moves"`
	Score          int    `json:"score"`
	MaxTile        int    `json:"maxTile"`
	EffectiveMoves int    `json:"effectiveMoves"`
	Over           bool   `json:"over"`
	Board          []int  `json:"board"`
}

type goldenRules struct {
	Size              int `json:"size"`
	FourChance        int `json:"fourChance"`
	MaxMoves          int `json:"maxMoves"`
	MaxMovesPerSecond int `json:"maxMovesPerSecond"`
}

type golden struct {
	Rules goldenRules  `json:"rules"`
	Games []goldenGame `json:"games"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	require.NoError(t, err)
	var record golden
	require.NoError(t, common.Unmarshal(data, &record))
	require.Len(t, record.Games, 3)
	return record
}

// golden.json 是浏览器端引擎打出来的整局记录(见 testdata/generate_golden.mjs)：一局认真玩到合出 2048、走不动为止，
// 一局随机乱按、记录里夹着大量没让盘面变化的移动和结束之后的移动，一局按几十下就主动结束。服务端从同一个种子重放，
// 得分、有效移动数、最大方块、是否结束和最终盘面都必须与浏览器一致，否则玩家看到的成绩和发下去的奖励就对不上。
func TestReplayMatchesBrowserEngine(t *testing.T) {
	for _, want := range loadGolden(t).Games {
		game, err := Replay(want.Seed, want.Moves)
		require.NoError(t, err, want.Name)

		assert.Equal(t, want.Score, game.Score, want.Name)
		assert.Equal(t, want.EffectiveMoves, game.Moves, want.Name)
		assert.Equal(t, want.MaxTile, game.MaxTile(), want.Name)
		assert.Equal(t, want.Over, game.Over, want.Name)
		assert.Equal(t, want.Board, game.board[:], want.Name)
	}
}

// 记录长度上限与移动速度上限决定浏览器什么时候必须结束提交、服务端按什么速度拒绝，两边必须一致。
func TestRulesMatchBrowserEngine(t *testing.T) {
	rules := loadGolden(t).Rules
	assert.Equal(t, Size, rules.Size)
	assert.Equal(t, fourChance, rules.FourChance)
	assert.Equal(t, MaxMoves, rules.MaxMoves)
	assert.Equal(t, MaxMovesPerSecond, rules.MaxMovesPerSecond)
}

// 合并规则：一行里相邻的相同方块从移动方向那一侧开始两两合并，每个方块一次移动里最多合并一次，得分是合并出的新方块之和。
// 移动之后恰好在一个空出来的格子里生成一个 2 或 4；没有让盘面变化的移动不生效、不生成方块、不消耗随机数。
func TestMoveMergesFromTheMovingSide(t *testing.T) {
	cases := map[string]struct {
		board     [cells]int
		direction Direction
		want      [cells]int // 移动之后、生成新方块之前的盘面
		gained    int
	}{
		"four equal tiles merge into two": {
			board:     [cells]int{2, 2, 2, 2},
			direction: Left,
			want:      [cells]int{4, 4, 0, 0},
			gained:    8,
		},
		"a merged tile does not merge again": {
			board:     [cells]int{4, 2, 2, 0},
			direction: Left,
			want:      [cells]int{4, 4, 0, 0},
			gained:    4,
		},
		"three equal tiles merge on the moving side": {
			board:     [cells]int{8, 8, 8, 0},
			direction: Right,
			want:      [cells]int{0, 0, 8, 16},
			gained:    16,
		},
		"gaps close before merging": {
			board:     [cells]int{2, 0, 0, 2, 0, 4, 0, 4},
			direction: Left,
			want:      [cells]int{4, 0, 0, 0, 8, 0, 0, 0},
			gained:    12,
		},
		"columns move up": {
			board:     [cells]int{2, 0, 0, 0, 2, 0, 0, 0, 4, 0, 0, 0, 4, 0, 0, 0},
			direction: Up,
			want:      [cells]int{4, 0, 0, 0, 8, 0, 0, 0},
			gained:    12,
		},
		"columns move down": {
			board:     [cells]int{0, 16, 0, 0, 0, 0, 0, 0, 0, 16, 0, 0, 0, 2, 0, 0},
			direction: Down,
			want:      [cells]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 32, 0, 0, 0, 2, 0, 0},
			gained:    32,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			game := &Game{board: c.board, rng: mulberry32{state: 1}}
			require.True(t, game.Move(c.direction))
			assert.Equal(t, c.gained, game.Score)
			assert.Equal(t, 1, game.Moves)
			spawned := 0
			for index, value := range game.board {
				if c.want[index] != 0 {
					assert.Equal(t, c.want[index], value, "cell %d", index)
					continue
				}
				if value != 0 {
					spawned++
					assert.Contains(t, []int{2, 4}, value, "cell %d", index)
				}
			}
			assert.Equal(t, 1, spawned, "每次生效的移动恰好生成一个新方块")
		})
	}

	stuck := &Game{board: [cells]int{2, 4, 8, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, rng: mulberry32{state: 1}}
	before := *stuck
	assert.False(t, stuck.Move(Left), "靠左排好、没有相同方块的一行向左移动不改变盘面")
	assert.Equal(t, before, *stuck, "不生效的移动不生成方块、不计步、不消耗随机数")
}

// 没有空格、也没有相邻的相同方块时结束；此后任何移动都不再生效。
func TestGameEndsWhenNoMoveChangesTheBoard(t *testing.T) {
	// 向左移动后右上角空出一格并补上新方块，此时盘面满了；新方块是 2 还是 4 都与邻居不同，一步也走不了。
	game := &Game{board: [cells]int{
		0, 8, 16, 32,
		64, 128, 256, 512,
		8, 16, 32, 64,
		128, 256, 512, 1024,
	}, rng: mulberry32{state: 1}}
	require.True(t, game.Move(Left))
	require.True(t, game.Over)
	for _, direction := range []Direction{Up, Right, Down, Left} {
		assert.False(t, game.Move(direction))
	}
}

// 移动记录来自浏览器，过长或含有 0-3 以外字符的记录一律拒绝，不做部分重放；空记录是开局就结束的一局。
func TestReplayRejectsMalformedRecords(t *testing.T) {
	empty, err := Replay(1, "")
	require.NoError(t, err)
	assert.Equal(t, 0, empty.Score)
	assert.False(t, empty.Over)

	_, err = Replay(1, strings.Repeat("0123", MaxMoves/4))
	require.NoError(t, err, "恰好 MaxMoves 个字符")

	for name, moves := range map[string]string{
		"longer than the limit": strings.Repeat("0", MaxMoves+1),
		"direction four":        "0124",
		"letter":                "01a",
		"non ascii":             "01２",
		"separator":             "0,1",
	} {
		_, err := Replay(1, moves)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}
}
