package minesweeper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gameFromRows 用一张画好的雷区开一局已经布好雷的扫雷：每个字符串是一行，'-' 是没翻开的空格、'*' 是雷。
func gameFromRows(rows ...string) *Game {
	cells := strings.Join(rows, "")
	return &Game{
		Level:  Level{Width: len(rows[0]), Height: len(rows), Mines: strings.Count(cells, "*")},
		Cells:  cells,
		Status: StatusPlaying,
	}
}

// fiveByFour 是一片 5×4、只有 (3,2) 一颗雷的雷区。从左上角翻开会展开 16 格，(4,2)、(3,3)、(4,3) 被雷周围的数字挡在后面。
func fiveByFour() *Game {
	return gameFromRows(
		"-----",
		"-----",
		"---*-",
		"-----",
	)
}

func keyOf(n byte) [32]byte {
	var key [32]byte
	key[0] = n
	return key
}

// 第一下点在哪里都不会踩雷：雷避开点的格子和它周围的 8 格，所以第一下翻开总是 0、能展开一片，布下的雷数正好是这个难度的雷数。
// 同一个密钥、同一个第一下总是布出同一片雷区。
func TestFirstRevealIsAlwaysSafe(t *testing.T) {
	for _, level := range Levels {
		clicks := [][2]int{
			{0, 0}, {level.Width - 1, 0}, {0, level.Height - 1}, {level.Width - 1, level.Height - 1},
			{level.Width / 2, level.Height / 2}, {1, level.Height / 2},
		}
		for _, click := range clicks {
			for seed := byte(0); seed < 4; seed++ {
				game, err := New(level.Difficulty, keyOf(seed))
				require.NoError(t, err)
				revealed, err := game.Reveal([][2]int{click})
				require.NoError(t, err)
				assert.NotEqual(t, StatusLost, game.Status)
				require.NotEmpty(t, revealed)
				assert.Equal(t, [3]int{click[0], click[1], 0}, revealed[0], "level %d click %v: 第一下周围没有雷", level.Difficulty, click)

				mines := game.MineCells()
				assert.Len(t, mines, level.Mines, "level %d click %v", level.Difficulty, click)
				for _, mine := range mines {
					nearX := mine[0] >= click[0]-1 && mine[0] <= click[0]+1
					nearY := mine[1] >= click[1]-1 && mine[1] <= click[1]+1
					assert.False(t, nearX && nearY, "level %d click %v: mine %v 在第一下周围", level.Difficulty, click, mine)
				}

				again, err := New(level.Difficulty, keyOf(seed))
				require.NoError(t, err)
				_, err = again.Reveal([][2]int{click})
				require.NoError(t, err)
				assert.Equal(t, game.Cells, again.Cells, "同一个密钥、同一个第一下布出同一片雷区")
			}
		}
	}
}

// 格子不够空出第一下周围的 3×3 时只避开点的那一格。3×3 的雷区放 5 颗雷时角上的 2×2 还能空出来，翻开角就展开这 4 格；
// 放 8 颗雷时只剩点的那一格，翻开就是 8。两种情况翻开的都是全部不是雷的格子，直接赢了。
func TestFirstRevealOnACrowdedBoard(t *testing.T) {
	cases := map[string]struct {
		mines int
		click [2]int
		want  [][3]int
	}{
		"the corner 2x2 is left free":         {mines: 5, click: [2]int{0, 0}, want: [][3]int{{0, 0, 0}, {1, 0, 2}, {0, 1, 2}, {1, 1, 5}}},
		"only the clicked cell can be spared": {mines: 8, click: [2]int{1, 1}, want: [][3]int{{1, 1, 8}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			game := &Game{Level: Level{Width: 3, Height: 3, Mines: c.mines}, Cells: "---------", Status: StatusPlaying}
			revealed, err := game.Reveal([][2]int{c.click})
			require.NoError(t, err)
			assert.Equal(t, c.want, revealed)
			assert.Equal(t, StatusWon, game.Status)
			assert.Len(t, game.MineCells(), c.mines)
		})
	}
}

// 翻到周围没有雷的格子会连锁展开：它周围的格子都翻开，其中又是 0 的继续展开；展开停在周围有雷的数字上，被数字挡在后面的
// 格子不会翻开。翻开剩下的所有空格就赢了，结束后才能看到雷的位置。
func TestRevealFloodFillsZerosAndWins(t *testing.T) {
	game := fiveByFour()
	revealed, err := game.Reveal([][2]int{{0, 0}})
	require.NoError(t, err)
	assert.ElementsMatch(t, [][3]int{
		{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}, {4, 0, 0},
		{0, 1, 0}, {1, 1, 0}, {2, 1, 1}, {3, 1, 1}, {4, 1, 1},
		{0, 2, 0}, {1, 2, 0}, {2, 2, 1},
		{0, 3, 0}, {1, 3, 0}, {2, 3, 1},
	}, revealed)
	assert.Equal(t, StatusPlaying, game.Status)
	assert.ElementsMatch(t, revealed, game.Revealed(), "刷新页面后恢复出的正是翻开过的格子")

	revealed, err = game.Reveal([][2]int{{4, 2}, {3, 3}, {4, 3}})
	require.NoError(t, err)
	assert.Equal(t, [][3]int{{4, 2, 1}, {3, 3, 1}, {4, 3, 1}}, revealed)
	assert.Equal(t, StatusWon, game.Status)
	assert.Equal(t, [][2]int{{3, 2}}, game.MineCells())
	_, exploded := game.Exploded()
	assert.False(t, exploded)
	assert.Len(t, game.Revealed(), 19)
}

// 一次翻开几格(双击和弦)时按顺序翻：踩到雷就输了，后面的格子不再翻；先翻完所有不是雷的格子就赢了，排在后面、插错旗的那颗雷
// 不算。已经翻开的格子跳过。
func TestRevealBatchStopsWhenTheGameEnds(t *testing.T) {
	cases := map[string]struct {
		cells        [][2]int
		wantRevealed [][3]int
		wantStatus   string
	}{
		"a mine ends the game before the later cells":        {cells: [][2]int{{4, 2}, {3, 2}, {3, 3}}, wantRevealed: [][3]int{{4, 2, 1}}, wantStatus: StatusLost},
		"the last safe cell wins before the misflagged mine": {cells: [][2]int{{4, 2}, {3, 3}, {4, 3}, {3, 2}}, wantRevealed: [][3]int{{4, 2, 1}, {3, 3, 1}, {4, 3, 1}}, wantStatus: StatusWon},
		"cells already open are skipped":                     {cells: [][2]int{{0, 0}, {2, 1}, {4, 2}}, wantRevealed: [][3]int{{4, 2, 1}}, wantStatus: StatusPlaying},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			game := fiveByFour()
			_, err := game.Reveal([][2]int{{0, 0}})
			require.NoError(t, err)
			revealed, err := game.Reveal(c.cells)
			require.NoError(t, err)
			assert.Equal(t, c.wantRevealed, revealed)
			assert.Equal(t, c.wantStatus, game.Status)
			exploded, ok := game.Exploded()
			assert.Equal(t, c.wantStatus == StatusLost, ok)
			if ok {
				assert.Equal(t, [2]int{3, 2}, exploded)
			}
		})
	}
}

// 不合规则的翻开整个拒绝、局面不变，第一次翻开被拒绝时也不会布雷：没有格子、一次超过 9 格、有一格越界。结束的一局不能再翻。
func TestRevealRejectsInvalidMoves(t *testing.T) {
	tooMany := make([][2]int, MaxRevealCells+1)
	for i := range tooMany {
		tooMany[i] = [2]int{i % 9, i / 9}
	}
	cases := map[string][][2]int{
		"no cells":                {},
		"more cells than a chord": tooMany,
		"x beyond the right edge": {{0, 0}, {9, 0}},
		"y above the top edge":    {{0, -1}},
		"y below the bottom edge": {{3, 9}},
		"x before the left edge":  {{-1, 4}},
	}
	for name, cells := range cases {
		t.Run(name, func(t *testing.T) {
			game, err := New(1, keyOf(1))
			require.NoError(t, err)
			before := *game
			_, err = game.Reveal(cells)
			assert.ErrorIs(t, err, ErrInvalidMove)
			assert.Equal(t, before, *game)
		})
	}

	game := fiveByFour()
	_, err := game.Reveal([][2]int{{3, 2}})
	require.NoError(t, err)
	require.Equal(t, StatusLost, game.Status)
	_, err = game.Reveal([][2]int{{0, 0}})
	assert.ErrorIs(t, err, ErrInvalidMove, "踩雷之后这局结束，不能再翻")
}
