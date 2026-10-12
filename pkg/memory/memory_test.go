package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keyOf(n byte) [32]byte {
	var key [32]byte
	key[0] = n
	return key
}

// threePairs 开一局牌面为 0 1 2 / 2 1 0 的记忆翻牌，位置 0 与 5、1 与 4、2 与 3 是一对，步数上限 4。
func threePairs() *Game {
	layout := []int{0, 1, 2, 2, 1, 0}
	return &Game{
		Level:   Level{Rows: 2, Cols: 3, Pairs: 3, MoveLimit: 4},
		Layout:  layout,
		Matched: make([]bool, len(layout)),
		First:   -1,
		Status:  StatusPlaying,
	}
}

// 每个难度发满 rows×cols 张牌：选中的每种图案正好两张，图案都在 0..17 之内；同一个密钥发出同一副牌。
func TestNewDealsEveryPairTwice(t *testing.T) {
	for _, level := range Levels {
		for seed := byte(0); seed < 4; seed++ {
			game, err := New(level.Difficulty, keyOf(seed))
			require.NoError(t, err)
			require.Len(t, game.Layout, level.Rows*level.Cols)
			require.Len(t, game.Layout, 2*level.Pairs)
			counts := map[int]int{}
			for _, symbol := range game.Layout {
				require.True(t, symbol >= 0 && symbol < SymbolCount, "symbol %d", symbol)
				counts[symbol]++
			}
			assert.Len(t, counts, level.Pairs)
			for symbol, count := range counts {
				assert.Equal(t, 2, count, "level %d symbol %d", level.Difficulty, symbol)
			}
			assert.Equal(t, -1, game.First)
			assert.Equal(t, StatusPlaying, game.Status)
			assert.Empty(t, game.MatchedCards())

			again, err := New(level.Difficulty, keyOf(seed))
			require.NoError(t, err)
			assert.Equal(t, game.Layout, again.Layout)
		}
	}
	_, err := New(4, keyOf(0))
	assert.ErrorIs(t, err, ErrInvalidDifficulty)
}

// 一步翻两张：第一张只告诉图案，第二张完成这一步，并告诉两张有没有配上、第一张是哪张。配上的牌一直朝上。
func TestFlipCompletesAMoveOnTheSecondCard(t *testing.T) {
	game := threePairs()
	first, err := game.Flip(0)
	require.NoError(t, err)
	assert.Equal(t, Flip{Index: 0, Symbol: 0, First: true}, first)
	assert.Equal(t, 0, game.Moves)

	miss, err := game.Flip(1)
	require.NoError(t, err)
	assert.Equal(t, Flip{Index: 1, Symbol: 1, PreviousIndex: 0, PreviousSymbol: 0}, miss)
	assert.Equal(t, 1, game.Moves)
	assert.Empty(t, game.MatchedCards(), "没配上的两张翻回去")

	_, err = game.Flip(5)
	require.NoError(t, err)
	hit, err := game.Flip(0)
	require.NoError(t, err)
	assert.Equal(t, Flip{Index: 0, Symbol: 0, Matched: true, PreviousIndex: 5, PreviousSymbol: 0}, hit)
	assert.Equal(t, 2, game.Moves)
	assert.Equal(t, [][2]int{{0, 0}, {5, 0}}, game.MatchedCards())
	assert.Equal(t, StatusPlaying, game.Status)
}

// 步数上限之内配完所有对就赢了；最后一步用完上限时配完了算赢、没配完算输，上限之前不会判输。
func TestMoveLimit(t *testing.T) {
	cases := map[string]struct {
		flips      []int
		wantStatus string
		wantMoves  int
	}{
		"perfect play needs one move per pair":         {flips: []int{0, 5, 1, 4, 2, 3}, wantStatus: StatusWon, wantMoves: 3},
		"the last allowed move completes the board":    {flips: []int{0, 1, 0, 5, 1, 4, 2, 3}, wantStatus: StatusWon, wantMoves: 4},
		"running out of moves loses":                   {flips: []int{0, 1, 0, 2, 0, 3, 1, 2}, wantStatus: StatusLost, wantMoves: 4},
		"one move before the limit is still playing":   {flips: []int{0, 1, 0, 2, 0, 3}, wantStatus: StatusPlaying, wantMoves: 3},
		"a first card at the limit waits for its pair": {flips: []int{0, 1, 0, 2, 0, 3, 1}, wantStatus: StatusPlaying, wantMoves: 3},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			game := threePairs()
			for _, index := range c.flips {
				_, err := game.Flip(index)
				require.NoError(t, err, "flip %d", index)
			}
			assert.Equal(t, c.wantStatus, game.Status)
			assert.Equal(t, c.wantMoves, game.Moves)
		})
	}
}

// 不能翻的牌整个拒绝、局面不变：位置越界、正翻开着的第一张、已经配上的牌、结束的一局。
func TestFlipRejectsInvalidCards(t *testing.T) {
	cases := map[string]struct {
		before []int
		index  int
	}{
		"before the first card":   {index: -1},
		"past the last card":      {index: 6},
		"the card already up":     {before: []int{2}, index: 2},
		"a matched card":          {before: []int{0, 5}, index: 5},
		"after the game was won":  {before: []int{0, 5, 1, 4, 2, 3}, index: 1},
		"after the game was lost": {before: []int{0, 1, 0, 2, 0, 3, 1, 2}, index: 4},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			game := threePairs()
			for _, index := range c.before {
				_, err := game.Flip(index)
				require.NoError(t, err)
			}
			before := *game
			before.Matched = append([]bool(nil), game.Matched...)
			_, err := game.Flip(c.index)
			assert.ErrorIs(t, err, ErrInvalidMove)
			assert.Equal(t, before, *game)
		})
	}
}
