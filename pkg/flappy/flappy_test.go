package flappy

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
	Ticks     int    `json:"ticks"`
	Flaps     []int  `json:"flaps"`
	Score     int    `json:"score"`
	FlapCount int    `json:"flapCount"`
	Over      bool   `json:"over"`
	Y         int    `json:"y"`
	VY        int    `json:"vy"`
	Pipes     []Pipe `json:"pipes"` // 浏览器引擎的 { x, gapTop, passed }
}

type goldenRules struct {
	TicksPerSecond  int `json:"ticksPerSecond"`
	MaxTicks        int `json:"maxTicks"`
	Scale           int `json:"scale"`
	ScreenWidth     int `json:"screenWidth"`
	GroundY         int `json:"groundY"`
	BirdX           int `json:"birdX"`
	BirdWidth       int `json:"birdWidth"`
	BirdHeight      int `json:"birdHeight"`
	BirdStartY      int `json:"birdStartY"`
	Gravity         int `json:"gravity"`
	FlapVelocity    int `json:"flapVelocity"`
	MaxFallSpeed    int `json:"maxFallSpeed"`
	PipeWidth       int `json:"pipeWidth"`
	PipeGap         int `json:"pipeGap"`
	PipeSpacing     int `json:"pipeSpacing"`
	PipeSpeed       int `json:"pipeSpeed"`
	FirstPipeX      int `json:"firstPipeX"`
	GapMinTop       int `json:"gapMinTop"`
	GapMaxTop       int `json:"gapMaxTop"`
	FlapWindowTicks int `json:"flapWindowTicks"`
	HumanFlapLimit  int `json:"humanFlapLimit"`
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

func replayGolden(t *testing.T, want goldenGame) *Game {
	t.Helper()
	flaps, err := DecodeFlaps(want.Flaps)
	require.NoError(t, err, want.Name)
	game, err := Replay(want.Seed, want.Ticks, flaps)
	require.NoError(t, err, want.Name)
	return game
}

// golden.json 是浏览器端引擎打出来的整局记录(见 testdata/generate_golden.mjs)：一局按人的节奏穿过 40 根管道后掉到地上，
// 一局随机乱拍撞上第一根管道，一局是拍翅膀快过人类的脚本、飞了 40 秒主动结束。服务端从同一个种子重放，得分、帧数、
// 拍翅膀次数、鸟的位置与速度、场上的管道都必须与浏览器一致，否则玩家看到的成绩和发下去的奖励就对不上。
func TestReplayMatchesBrowserEngine(t *testing.T) {
	for _, want := range loadGolden(t).Games {
		game := replayGolden(t, want)

		assert.Equal(t, want.Score, game.Score, want.Name)
		assert.Equal(t, want.Ticks, game.Ticks, want.Name)
		assert.Equal(t, want.FlapCount, game.Flaps, want.Name)
		assert.Equal(t, want.Over, game.Over, want.Name)
		assert.Equal(t, want.Y, game.Y, want.Name)
		assert.Equal(t, want.VY, game.VY, want.Name)
		assert.Equal(t, want.Pipes, game.pipes, want.Name)
	}
}

// 物理与画面常量、时长上限和速度检查的窗口决定了两端怎么推进、服务端按什么拒绝，必须与浏览器一致。
func TestRulesMatchBrowserEngine(t *testing.T) {
	assert.Equal(t, goldenRules{
		TicksPerSecond:  TicksPerSecond,
		MaxTicks:        MaxTicks,
		Scale:           Scale,
		ScreenWidth:     ScreenWidth,
		GroundY:         GroundY,
		BirdX:           BirdX,
		BirdWidth:       BirdWidth,
		BirdHeight:      BirdHeight,
		BirdStartY:      BirdStartY,
		Gravity:         Gravity,
		FlapVelocity:    FlapVelocity,
		MaxFallSpeed:    MaxFallSpeed,
		PipeWidth:       PipeWidth,
		PipeGap:         PipeGap,
		PipeSpacing:     PipeSpacing,
		PipeSpeed:       PipeSpeed,
		FirstPipeX:      FirstPipeX,
		GapMinTop:       GapMinTop,
		GapMaxTop:       GapMaxTop,
		FlapWindowTicks: FlapWindowTicks,
		HumanFlapLimit:  HumanFlapLimit,
	}, loadGolden(t).Rules)
}

// 开局鸟悬停、管道不动，帧数也不走，直到第一次拍翅膀；之后每帧重力让下落速度加 25，最快 1000，拍翅膀直接把速度设为 -460。
func TestBirdHoversUntilTheFirstFlap(t *testing.T) {
	game := New(1)
	pipes := append([]Pipe(nil), game.pipes...)
	for range 30 {
		game.Tick(false)
	}
	assert.Equal(t, 0, game.Ticks)
	assert.Equal(t, BirdStartY*Scale, game.Y)
	assert.Equal(t, pipes, game.pipes)

	game.Tick(true)
	assert.Equal(t, 1, game.Ticks)
	assert.Equal(t, FlapVelocity, game.VY)
	assert.Equal(t, BirdStartY*Scale+FlapVelocity, game.Y)
	assert.Equal(t, FirstPipeX*Scale-PipeSpeed*Scale, game.pipes[0].X)

	speeds := []int{}
	for range 62 {
		game.Tick(false)
		speeds = append(speeds, game.VY)
	}
	assert.Equal(t, -435, speeds[0])
	assert.Equal(t, 990, speeds[57])
	assert.Equal(t, MaxFallSpeed, speeds[58], "下落速度到 1000 封顶")
	assert.Equal(t, MaxFallSpeed, speeds[61])
}

// 管道右沿越过鸟的左沿(严格小于)才算穿过；碰撞盒只是边挨着边不算撞上。碰到地面、飞出顶部结束。
func TestPassingAndCollisionBoundaries(t *testing.T) {
	// 让鸟在第 0 帧拍一下，再把一根缺口很宽的管道摆到指定位置，鸟在缺口里。
	started := func(pipe Pipe) *Game {
		game := New(1)
		game.Tick(true)
		game.pipes = []Pipe{pipe, {X: 100000, GapTop: 100 * Scale}}
		return game
	}

	// 下一帧管道右沿恰好与鸟左沿重合：还没穿过；再下一帧才穿过。
	passing := started(Pipe{X: (BirdX-PipeWidth)*Scale + PipeSpeed*Scale, GapTop: 100 * Scale})
	passing.Tick(false)
	assert.Equal(t, BirdX*Scale, passing.pipes[0].X+PipeWidth*Scale)
	assert.Equal(t, 0, passing.Score)
	passing.Tick(false)
	assert.Equal(t, 1, passing.Score)
	assert.False(t, passing.Over)

	// 缺口远在鸟的下面，管道的左右沿与鸟边挨着边都不算撞上，多进一个单位就撞上了。
	low := 300 * Scale
	for name, c := range map[string]struct {
		x    int // 这一帧移动之后管道的左沿
		over bool
	}{
		"right edge touching the bird": {x: (BirdX - PipeWidth) * Scale, over: false},
		"right edge inside the bird":   {x: (BirdX-PipeWidth)*Scale + 1, over: true},
		"left edge touching the bird":  {x: (BirdX + BirdWidth) * Scale, over: false},
		"left edge inside the bird":    {x: (BirdX+BirdWidth)*Scale - 1, over: true},
	} {
		game := started(Pipe{X: c.x + PipeSpeed*Scale, GapTop: low})
		game.Tick(false)
		assert.Equal(t, c.over, game.Over, name)
	}

	ceiling := New(1)
	ceiling.Tick(true)
	for !ceiling.Over {
		ceiling.Tick(true)
	}
	assert.Less(t, ceiling.Y, 0, "一直拍翅膀会飞出顶部")

	ground := New(1)
	ground.Tick(true)
	for !ground.Over {
		ground.Tick(false)
	}
	assert.GreaterOrEqual(t, ground.Y+BirdHeight*Scale, GroundY*Scale, "不拍翅膀会落到地上")
	assert.Less(t, ground.Y-ground.VY+BirdHeight*Scale, GroundY*Scale, "上一帧还在地面以上")
}

// 速度检查看任意 1 秒(60 帧)内拍了几次翅膀：golden 里按人的节奏飞的一局和乱拍的一局都在人类极限以内，脚本远远超出。
// 窗口按帧计算，相隔不到 60 帧的算在同一个窗口里，恰好相隔 60 帧就不算。
func TestReplayMeasuresPeakFlapRate(t *testing.T) {
	for _, want := range loadGolden(t).Games {
		game := replayGolden(t, want)
		if want.Name == "script" {
			assert.Greater(t, game.PeakFlaps, HumanFlapLimit, want.Name)
		} else {
			assert.LessOrEqual(t, game.PeakFlaps, HumanFlapLimit, want.Name)
		}
	}

	every := func(start, step, count int) []int {
		flaps := make([]int, count)
		for i := range flaps {
			flaps[i] = start + i*step
		}
		return flaps
	}
	for name, c := range map[string]struct {
		flaps []int
		want  int
	}{
		"thirteen within a second":            {flaps: every(0, 4, 13), want: 13},
		"thirteen spanning exactly 60 frames": {flaps: every(0, 5, 13), want: 12},
		"two exactly a second apart":          {flaps: []int{0, 60}, want: 1},
		"two within a second":                 {flaps: []int{0, 59}, want: 2},
		"none":                                {want: 0},
	} {
		assert.Equal(t, c.want, peakFlaps(c.flaps), name)
	}
}

// 撞上之后再多报帧数、多塞拍翅膀都不会改变成绩，也不计入速度检查。
func TestReplayIgnoresFlapsAfterTheCrash(t *testing.T) {
	var crash goldenGame
	for _, game := range loadGolden(t).Games {
		if game.Name == "chaos-crash" {
			crash = game
		}
	}
	require.True(t, crash.Over)
	flaps, err := DecodeFlaps(crash.Flaps)
	require.NoError(t, err)
	last := flaps[len(flaps)-1]
	for i := 1; i <= 20; i++ {
		flaps = append(flaps, last+crash.Ticks+i)
	}
	game, err := Replay(crash.Seed, crash.Ticks+last+100, flaps)
	require.NoError(t, err)
	assert.Equal(t, crash.Score, game.Score)
	assert.Equal(t, crash.Ticks, game.Ticks)
	assert.Equal(t, crash.FlapCount, game.Flaps)
	assert.LessOrEqual(t, game.PeakFlaps, HumanFlapLimit)
}

// 拍翅膀记录来自浏览器，结构不对、越界、一帧拍两次或第 0 帧没拍的记录一律拒绝，不做部分重放；
// 没拍过翅膀就结束的一局成绩为 0。
func TestReplayRejectsMalformedRecords(t *testing.T) {
	idle, err := Replay(1, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, idle.Score)
	assert.Equal(t, 0, idle.Ticks)

	_, err = Replay(1, 10, []int{0, 9})
	require.NoError(t, err)

	cases := map[string]struct {
		ticks int
		flaps []int
	}{
		"negative ticks":            {ticks: -1},
		"longer than an hour":       {ticks: MaxTicks + 1},
		"ticks without any flap":    {ticks: 10},
		"first flap after frame 0":  {ticks: 10, flaps: []int{3}},
		"flap on unplayed tick":     {ticks: 10, flaps: []int{0, 10}},
		"two flaps in one frame":    {ticks: 10, flaps: []int{0, 4, 4}},
		"flaps out of order":        {ticks: 10, flaps: []int{0, 5, 4}},
		"more flaps than ticks":     {ticks: 1, flaps: []int{0, 1}},
		"flaps without any ticks":   {ticks: 0, flaps: []int{0}},
		"negative flap frame":       {ticks: 10, flaps: []int{0, -1}},
		"flap frame far beyond end": {ticks: 10, flaps: []int{0, MaxTicks}},
	}
	for name, c := range cases {
		_, err := Replay(1, c.ticks, c.flaps)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}

	for name, encoded := range map[string][]int{
		"negative delta":         {0, -1},
		"beyond an hour of play": {MaxTicks, 1},
		"delta beyond an hour":   {MaxTicks + 1},
	} {
		_, err := DecodeFlaps(encoded)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}
}
