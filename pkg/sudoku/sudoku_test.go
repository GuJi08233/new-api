package sudoku

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 维基百科"数独"词条里的题目与它唯一的解。
const (
	wikiPuzzle = "530070000" + "600195000" + "098000060" +
		"800060003" + "400803001" + "700020006" +
		"060000280" + "000419005" + "000080079"
	wikiSolution = "534678912" + "672195348" + "198342567" +
		"859761423" + "426853791" + "713924856" +
		"961537284" + "287419635" + "345286179"
)

func cellsOf(t *testing.T, text string) [cellCount]uint8 {
	t.Helper()
	require.Len(t, text, cellCount)
	var cells [cellCount]uint8
	for index := range cells {
		require.True(t, text[index] >= '0' && text[index] <= '9')
		cells[index] = text[index] - '0'
	}
	return cells
}

// 判唯一解用的数解：唯一解的题数出 1 个；空盘面有无数个解，数到上限 2 个就停。
func TestCountSolutions(t *testing.T) {
	budget := math.MaxInt
	count, complete := countSolutions(cellsOf(t, wikiPuzzle), 2, &budget)
	require.True(t, complete)
	assert.Equal(t, 1, count)

	budget = math.MaxInt
	count, complete = countSolutions(cellsOf(t, strings.Repeat("0", cellCount)), 2, &budget)
	require.True(t, complete)
	assert.Equal(t, 2, count)
}

// 每个难度按种子出的题：给定数在这个难度的范围内，谜底是符合题目的完整解，而且题目只有这一个解(不限尝试次数地数到 2 个)。
// 同一个种子出同一道题。
func TestGenerateMakesUniquePuzzlesPerDifficulty(t *testing.T) {
	for _, level := range Levels {
		for seed := uint64(0); seed < 20; seed++ {
			puzzle, err := Generate(level.Difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, level.Difficulty, puzzle.Difficulty)
			givens := puzzle.Givens()
			assert.True(t, givens >= level.MinGivens && givens <= level.MaxGivens, "level %d seed %d: %d givens", level.Difficulty, seed, givens)
			assert.True(t, Check(puzzle.Puzzle, puzzle.Solution), "level %d seed %d: 谜底符合题目、每行每列每宫各一次", level.Difficulty, seed)

			budget := math.MaxInt
			count, complete := countSolutions(cellsOf(t, puzzle.Puzzle), 2, &budget)
			require.True(t, complete)
			assert.Equal(t, 1, count, "level %d seed %d 的题必须唯一解", level.Difficulty, seed)

			again, err := Generate(level.Difficulty, seed)
			require.NoError(t, err)
			assert.Equal(t, puzzle, again)
		}
	}
	_, err := Generate(4, 0)
	assert.ErrorIs(t, err, ErrInvalidDifficulty)
}

// 交卷判定：谜底通过；把 1 和 2 全部对调后仍是合法的数独，但改了给出的数字，不通过；同一行里对调两个空格，行没问题、列重复了；
// 每行每列都是 1-9 的拉丁方，宫里有重复；有空格、有非数字、长度不对都不通过。
func TestCheck(t *testing.T) {
	relabeled := strings.Map(func(r rune) rune {
		switch r {
		case '1':
			return '2'
		case '2':
			return '1'
		}
		return r
	}, wikiSolution)
	// 第一行的第 3、4 格(谜底是 4 与 6)在题目里都是空格。
	swapped := wikiSolution[:2] + wikiSolution[3:4] + wikiSolution[2:3] + wikiSolution[4:]
	var latin strings.Builder
	for row := 0; row < 9; row++ {
		for col := 0; col < 9; col++ {
			latin.WriteByte(byte('1' + (row+col)%9))
		}
	}
	cases := map[string]struct {
		puzzle string
		answer string
		want   bool
	}{
		"the solution":                        {puzzle: wikiPuzzle, answer: wikiSolution, want: true},
		"a valid grid that changes a given":   {puzzle: wikiPuzzle, answer: relabeled, want: false},
		"two cells swapped within a row":      {puzzle: wikiPuzzle, answer: swapped, want: false},
		"a latin square repeating in a box":   {puzzle: strings.Repeat("0", cellCount), answer: latin.String(), want: false},
		"an empty cell":                       {puzzle: wikiPuzzle, answer: wikiSolution[:2] + "0" + wikiSolution[3:], want: false},
		"a character that is not a digit":     {puzzle: wikiPuzzle, answer: wikiSolution[:2] + "x" + wikiSolution[3:], want: false},
		"an answer shorter than the grid":     {puzzle: wikiPuzzle, answer: wikiSolution[:80], want: false},
		"any valid grid solves an empty grid": {puzzle: strings.Repeat("0", cellCount), answer: relabeled, want: true},
	}
	for name, c := range cases {
		assert.Equal(t, c.want, Check(c.puzzle, c.answer), name)
	}
}

// 出一道题的耗时：go test -bench Generate ./pkg/sudoku/
func BenchmarkGenerate(b *testing.B) {
	for _, level := range Levels {
		b.Run(fmt.Sprintf("difficulty-%d", level.Difficulty), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Generate(level.Difficulty, uint64(i)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
