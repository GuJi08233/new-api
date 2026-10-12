package breakout

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestReplayMatchesBrowserPhysicsAndScoring(t *testing.T) {
	data, err := os.ReadFile("testdata/golden.json")
	require.NoError(t, err)
	var records []struct {
		Name   string
		Seed   uint32
		Inputs []int
		Game   Game
	}
	require.NoError(t, common.Unmarshal(data, &records))
	require.NotEmpty(t, records)
	for _, record := range records {
		t.Run(record.Name, func(t *testing.T) {
			game, err := Replay(record.Seed, record.Inputs)
			require.NoError(t, err)
			assert.Equal(t, record.Game, *game)
		})
	}
}

func TestReplayBoundsAndGameOver(t *testing.T) {
	for _, inputs := range [][]int{nil, {-1}, {Width + 1}, make([]int, MaxTicks+1)} {
		_, err := Replay(1, inputs)
		assert.ErrorIs(t, err, ErrReplay)
	}
	game := New(1)
	game.Wait = 0
	game.Balls = []Ball{{X: 200, Y: Height + Radius, VX: 1, VY: 4}}
	game.Lives = 1
	game.Step(200)
	require.True(t, game.Over)
	assert.Zero(t, game.Lives)
	ticks, score := game.Ticks, game.Score
	game.Step(200)
	assert.Equal(t, ticks, game.Ticks)
	assert.Equal(t, score, game.Score)
}

func TestBrickBonusesAndClearBonus(t *testing.T) {
	game := New(1)
	game.Wait = 0
	game.Balls = []Ball{{X: BrickLeft + 10, Y: BrickTop + BrickHeight + Radius + 1, VX: 1, VY: -4}}
	game.Bricks[0] = 1
	game.Step(100)
	assert.Equal(t, 15, game.Score)
	assert.Equal(t, 1, game.Broken)
	assert.Len(t, game.Balls, 2)
	assert.Equal(t, 15*FPS, game.Wide)
	game = New(1)
	game.Wait = 0
	game.Bricks = make([]int, Rows*Columns)
	game.Bricks[1] = 1
	game.Broken = Rows*Columns - 1
	game.Balls = []Ball{{X: BrickLeft + BrickWidth + BrickGap + 10, Y: BrickTop + BrickHeight + Radius + 1, VX: 1, VY: -4}}
	game.Step(100)
	assert.True(t, game.Won)
	assert.True(t, game.Over)
	assert.Equal(t, 115, game.Score)
}
