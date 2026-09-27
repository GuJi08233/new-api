package puzzle15

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func solvedBoard(size int) []int {
	board := make([]int, size*size)
	for index := range board[:len(board)-1] {
		board[index] = index + 1
	}
	return board
}

// 每个难度按种子打乱的盘面：是 0..n²-1 的一个排列，能排好，还没排好；同一个种子打乱出同一个盘面。
func TestNewDealsSolvableUnsolvedBoards(t *testing.T) {
	for _, level := range Levels {
		for seed := uint64(0); seed < 20; seed++ {
			game, err := New(level.Difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, level.Difficulty, game.Difficulty)
			assert.Equal(t, level.Size, game.Size)
			tiles := slices.Sorted(slices.Values(game.Board))
			for index, tile := range tiles {
				require.Equal(t, index, tile, "level %d seed %d: 盘面必须是 0..n²-1 的排列", level.Difficulty, seed)
			}
			assert.True(t, Solvable(game.Size, game.Board), "level %d seed %d", level.Difficulty, seed)
			assert.False(t, Solved(game.Board), "level %d seed %d", level.Difficulty, seed)

			again, err := New(level.Difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, game.Board, again.Board)
		}
	}
	_, err := New(4, 0)
	assert.ErrorIs(t, err, ErrInvalidDifficulty)
}

// 3×3 穷举：从排好的盘面出发按重放的走法走得到的盘面正好 9!/2 = 181440 个，Solvable 对它们都为真；全部 9! 个排列里 Solvable 为真的
// 也正好这么多，所以走不到的盘面 Solvable 都为假。
func TestSolvableMatchesReachability3x3(t *testing.T) {
	start := [9]int{1, 2, 3, 4, 5, 6, 7, 8, 0}
	reachable := map[[9]int]bool{start: true}
	queue := [][9]int{start}
	for len(queue) > 0 {
		board := queue[0]
		queue = queue[1:]
		for _, move := range "UDLR" {
			moved, err := (&Game{Size: 3, Board: board[:]}).Replay(string(move))
			if err != nil {
				continue // 这一步走出盘面
			}
			if next := [9]int(moved); !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	require.Len(t, reachable, 181440)
	for board := range reachable {
		require.True(t, Solvable(3, board[:]), "%v 走得到，必须判为可解", board)
	}

	solvable := 0
	permutation := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
	for {
		if Solvable(3, permutation) {
			solvable++
		}
		// 按字典序换到下一个排列。
		i := len(permutation) - 2
		for i >= 0 && permutation[i] >= permutation[i+1] {
			i--
		}
		if i < 0 {
			break
		}
		j := len(permutation) - 1
		for permutation[j] <= permutation[i] {
			j--
		}
		permutation[i], permutation[j] = permutation[j], permutation[i]
		slices.Reverse(permutation[i+1:])
	}
	assert.Equal(t, 181440, solvable)
}

// 边长为偶数时空格所在的行也算数：4×4 排好后空格往上走一步，逆序数变成奇数，仍然能排好；对调最后两块(4×4 是 Sam Loyd
// 的 14-15 难题)就排不好，空格再怎么走也排不好。边长为奇数时只看逆序数。
func TestSolvableOnEvenAndOddWidths(t *testing.T) {
	cases := []struct {
		size          int
		swapLastTiles bool
		moves         string
		want          bool
	}{
		{size: 4, want: true},
		{size: 4, moves: "U", want: true},
		{size: 4, moves: "ULLURDDR", want: true},
		{size: 4, swapLastTiles: true, want: false},
		{size: 4, swapLastTiles: true, moves: "U", want: false},
		{size: 4, swapLastTiles: true, moves: "ULLURDDR", want: false},
		{size: 5, moves: "UL", want: true},
		{size: 5, swapLastTiles: true, want: false},
		{size: 5, swapLastTiles: true, moves: "U", want: false},
	}
	for _, c := range cases {
		board := solvedBoard(c.size)
		if c.swapLastTiles {
			last := len(board) - 2
			board[last-1], board[last] = board[last], board[last-1]
		}
		moved, err := (&Game{Size: c.size, Board: board}).Replay(c.moves)
		require.NoError(t, err)
		assert.Equal(t, c.want, Solvable(c.size, moved), "size %d swap %v moves %q", c.size, c.swapLastTiles, c.moves)
	}
}

// 重放按空格的移动一步步走：U 上、D 下、L 左、R 右，走完排好才算解开。走出盘面、不认识的字符(包括小写)、超过两万步都不能重放；
// 重放不改动开局盘面。
func TestReplay(t *testing.T) {
	start := []int{1, 2, 3, 4, 5, 6, 7, 0, 8}
	game := &Game{Difficulty: 1, Size: 3, Board: slices.Clone(start)}
	backAndForth := strings.Repeat("LR", MaxMoves/2)
	cases := map[string]struct {
		moves      string
		want       []int
		wantSolved bool
	}{
		"no moves":                       {moves: "", want: start},
		"right solves it":                {moves: "R", want: solvedBoard(3), wantSolved: true},
		"left then up":                   {moves: "LU", want: []int{1, 2, 3, 0, 5, 6, 4, 7, 8}},
		"back and forth up to the limit": {moves: backAndForth, want: start},
		"down leaves the board":          {moves: "D"},
		"left twice leaves the board":    {moves: "LL"},
		"lowercase is not a move":        {moves: "r"},
		"an unknown character":           {moves: "RX"},
		"one move past the limit":        {moves: backAndForth + "R"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			board, err := game.Replay(c.moves)
			if c.want == nil {
				assert.ErrorIs(t, err, ErrInvalidMoves)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, board)
			assert.Equal(t, c.wantSolved, Solved(board))
		})
	}
	assert.Equal(t, start, game.Board)
}
