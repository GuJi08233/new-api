package snake

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenGame struct {
	Name      string `json:"name"`
	Seed      uint32 `json:"seed"`
	Steps     int    `json:"steps"`
	Inputs    []int  `json:"inputs"`
	Score     int    `json:"score"`
	Length    int    `json:"length"`
	ElapsedMs int    `json:"elapsedMs"`
	Over      bool   `json:"over"`
	Direction int    `json:"direction"`
	Food      int    `json:"food"`
	Body      []int  `json:"body"`
}

type goldenRules struct {
	Width         int `json:"width"`
	Height        int `json:"height"`
	StartLength   int `json:"startLength"`
	StartX        int `json:"startX"`
	StartY        int `json:"startY"`
	MaxSteps      int `json:"maxSteps"`
	BaseStepMs    int `json:"baseStepMs"`
	StepMsPerFood int `json:"stepMsPerFood"`
	MinStepMs     int `json:"minStepMs"`
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

// golden.json 是浏览器端引擎打出来的整局记录(见 testdata/generate_golden.mjs)：一局沿最短路吃到 45 个、加速到最快步长后
// 一头撞死，一局随机乱转、记录里夹着大量同向和反向的转向，一局吃了几口就主动结束。服务端从同一个种子重放，
// 得分、步数、游戏内时长、蛇身、朝向和食物位置都必须与浏览器一致，否则玩家看到的成绩和发下去的奖励就对不上。
func TestReplayMatchesBrowserEngine(t *testing.T) {
	for _, want := range loadGolden(t).Games {
		inputs, err := DecodeInputs(want.Inputs)
		require.NoError(t, err, want.Name)
		game, err := Replay(want.Seed, want.Steps, inputs)
		require.NoError(t, err, want.Name)

		assert.Equal(t, want.Score, game.Score, want.Name)
		assert.Equal(t, want.Length, game.Length(), want.Name)
		assert.Equal(t, want.Steps, game.Steps, want.Name)
		assert.Equal(t, want.ElapsedMs, game.ElapsedMs, want.Name)
		assert.Equal(t, want.Over, game.Over, want.Name)
		assert.Equal(t, want.Direction, int(game.direction), want.Name)
		assert.Equal(t, want.Food, game.food, want.Name)
		assert.Equal(t, want.Body, game.body, want.Name)
	}
}

// 场地、开局位置、步数上限与步长公式决定了两端怎么推进、服务端怎么核对游戏内时长，必须与浏览器一致。
func TestRulesMatchBrowserEngine(t *testing.T) {
	rules := loadGolden(t).Rules
	assert.Equal(t, Width, rules.Width)
	assert.Equal(t, Height, rules.Height)
	assert.Equal(t, StartLength, rules.StartLength)
	assert.Equal(t, startX, rules.StartX)
	assert.Equal(t, startY, rules.StartY)
	assert.Equal(t, MaxSteps, rules.MaxSteps)
	assert.Equal(t, baseStepMs, rules.BaseStepMs)
	assert.Equal(t, stepMsPerFood, rules.StepMsPerFood)
	assert.Equal(t, minStepMs, rules.MinStepMs)
}

// 步长 = max(70, 150 - 4 × 得分) 毫秒：越吃越快，吃到 20 个以后不再加速。每一步按它开始前的得分计时。
func TestStepDurationSpeedsUpWithScore(t *testing.T) {
	for score, want := range map[int]int{0: 150, 1: 146, 19: 74, 20: 70, 21: 70, 397: 70} {
		game := New(1)
		game.Score = score
		assert.Equal(t, want, game.StepMs(), "score %d", score)
	}

	// 食物挪到角落，两步都吃不到，按 150 毫秒各计一次；吃到之后的那一步才按新的得分计时。
	game := New(1)
	game.food = 0
	game.Step(NoTurn)
	game.Step(NoTurn)
	assert.Equal(t, 300, game.ElapsedMs)
	game.food = game.body[len(game.body)-1] + 1
	game.Step(NoTurn)
	require.Equal(t, 1, game.Score)
	assert.Equal(t, 450, game.ElapsedMs, "吃到食物的那一步仍按吃之前的得分计时")
	game.food = game.body[len(game.body)-1] + 1
	game.Step(NoTurn)
	assert.Equal(t, 596, game.ElapsedMs)
}

// 与当前方向相同或相反的转向不生效，蛇照原方向前进；生效的转向在这一步移动之前改变方向。
func TestTurnsThatRepeatOrReverseAreIgnored(t *testing.T) {
	game := New(1)
	head := func() (int, int) {
		cell := game.body[len(game.body)-1]
		return cell % Width, cell / Width
	}

	assert.False(t, game.Step(Left), "朝右时向左是掉头")
	x, y := head()
	assert.Equal(t, [2]int{5, 10}, [2]int{x, y})
	assert.False(t, game.Step(Right), "已经朝右")
	assert.True(t, game.Step(Up))
	x, y = head()
	assert.Equal(t, [2]int{6, 9}, [2]int{x, y})
	assert.False(t, game.Step(Down), "朝上时向下是掉头")
	assert.Equal(t, Up, game.direction)
}

// 头可以进入尾巴这一步空出来的格子；进入蛇身的其他格子、撞墙都结束，结束的那一步不改变蛇身。
func TestCollisionSparesTheCellTheTailLeaves(t *testing.T) {
	cell := func(x, y int) int { return y*Width + x }
	build := func(body []int, direction Direction) *Game {
		game := &Game{body: body, direction: direction, food: cell(0, 0)}
		for _, c := range body {
			game.occupied[c] = true
		}
		return game
	}

	// 四格绕成一个圈，头朝上正对着尾巴。
	loop := build([]int{cell(5, 5), cell(6, 5), cell(6, 6), cell(5, 6)}, Left)
	loop.Step(Up)
	assert.False(t, loop.Over)
	assert.Equal(t, []int{cell(6, 5), cell(6, 6), cell(5, 6), cell(5, 5)}, loop.body)

	// 多一节身体，头朝上撞到的就不是尾巴了。
	longer := build([]int{cell(4, 5), cell(5, 5), cell(6, 5), cell(6, 6), cell(5, 6)}, Left)
	before := append([]int(nil), longer.body...)
	longer.Step(Up)
	assert.True(t, longer.Over)
	assert.Equal(t, before, longer.body)

	wall := build([]int{cell(17, 3), cell(18, 3), cell(19, 3)}, Right)
	wall.Step(NoTurn)
	assert.True(t, wall.Over)
	assert.Equal(t, 1, wall.Steps, "撞墙的那一步也计入步数和游戏内时长")
	assert.Equal(t, 150, wall.ElapsedMs)
}

// 吃掉最后一个空格里的食物、蛇占满整个场地时结束，不会再去放食物。
func TestFillingTheBoardEndsTheGame(t *testing.T) {
	game := &Game{direction: Left, food: 0}
	// 除了左上角都是蛇身，头在 (1,0) 朝左正对着食物。
	for c := Width*Height - 1; c >= 1; c-- {
		game.body = append(game.body, c)
		game.occupied[c] = true
	}
	game.Score = len(game.body) - StartLength
	game.Step(NoTurn)
	assert.True(t, game.Over)
	assert.Equal(t, Width*Height, game.Length())
	assert.Equal(t, Width*Height-StartLength, game.Score)
	assert.Equal(t, -1, game.food)
}

// 转向记录来自浏览器，结构不对、越界、同一步转两次的记录一律拒绝，不做部分重放；没推进过的一局成绩为 0。
func TestReplayRejectsMalformedRecords(t *testing.T) {
	empty, err := Replay(1, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, empty.Score)
	assert.Equal(t, 0, empty.ElapsedMs)

	_, err = Replay(1, 10, []Input{{Step: 0, Direction: Up}, {Step: 9, Direction: Left}})
	require.NoError(t, err)

	cases := map[string]struct {
		steps  int
		inputs []Input
	}{
		"negative steps":         {steps: -1},
		"beyond the step limit":  {steps: MaxSteps + 1},
		"input on unplayed step": {steps: 10, inputs: []Input{{Step: 10, Direction: Up}}},
		"two turns in one step":  {steps: 10, inputs: []Input{{Step: 3, Direction: Up}, {Step: 3, Direction: Left}}},
		"inputs out of order":    {steps: 10, inputs: []Input{{Step: 5, Direction: Up}, {Step: 4, Direction: Left}}},
		"unknown direction":      {steps: 10, inputs: []Input{{Step: 1, Direction: Left + 1}}},
		"negative direction":     {steps: 10, inputs: []Input{{Step: 1, Direction: NoTurn}}},
		"more turns than steps":  {steps: 1, inputs: []Input{{Step: 0, Direction: Up}, {Step: 1, Direction: Left}}},
	}
	for name, c := range cases {
		_, err := Replay(1, c.steps, c.inputs)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}

	for name, encoded := range map[string][]int{
		"odd length":              {0, 1, 3},
		"negative delta":          {-1, 1},
		"beyond the step limit":   {MaxSteps, 1, 1, 1},
		"delta beyond step limit": {MaxSteps + 1, 1},
	} {
		_, err := DecodeInputs(encoded)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}
}
