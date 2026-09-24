package tetris

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenGame struct {
	Name   string   `json:"name"`
	Seed   uint32   `json:"seed"`
	Ticks  int      `json:"ticks"`
	Inputs []int    `json:"inputs"`
	Score  int      `json:"score"`
	Lines  int      `json:"lines"`
	Level  int      `json:"level"`
	Pieces int      `json:"pieces"`
	Over   bool     `json:"over"`
	Board  []string `json:"board"`
}

type goldenRules struct {
	PieceBoxes    []int     `json:"pieceBoxes"`
	SpawnCells    [][]int   `json:"spawnCells"`
	JlstzKicks    [][][]int `json:"jlstzKicks"`
	IKicks        [][][]int `json:"iKicks"`
	Gravity       []int     `json:"gravity"`
	LineScores    []int     `json:"lineScores"`
	LockDelay     int       `json:"lockDelay"`
	MaxLockResets int       `json:"maxLockResets"`
	LinesPerLevel int       `json:"linesPerLevel"`
	PreviewSize   int       `json:"previewSize"`
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
	require.Len(t, record.Games, 2)
	return record
}

func flattenPairs(pairs [][2]int) []int {
	flat := make([]int, 0, 2*len(pairs))
	for _, pair := range pairs {
		flat = append(flat, pair[0], pair[1])
	}
	return flat
}

func boardRows(g *Game) []string {
	rows := make([]string, 0, Height)
	for _, row := range g.board {
		var builder strings.Builder
		for _, cell := range row {
			builder.WriteByte('0' + cell)
		}
		rows = append(rows, builder.String())
	}
	return rows
}

// golden.json 是浏览器端引擎打出来的整局记录(见 testdata/generate_golden.mjs)：一局消到 150 行、
// 升到 16 级后顶出，一局乱按到中途主动结束。服务端从同一个种子重放，分数、消行、等级、方块数、
// 帧数和最终盘面都必须与浏览器一致，否则玩家看到的成绩和发下去的奖励就对不上。
func TestReplayMatchesBrowserEngine(t *testing.T) {
	for _, want := range loadGolden(t).Games {
		inputs, err := DecodeInputs(want.Inputs)
		require.NoError(t, err, want.Name)
		game, err := Replay(want.Seed, want.Ticks, inputs)
		require.NoError(t, err, want.Name)

		assert.Equal(t, want.Score, game.Score, want.Name)
		assert.Equal(t, want.Lines, game.Lines, want.Name)
		assert.Equal(t, want.Level, game.Level, want.Name)
		assert.Equal(t, want.Pieces, game.Pieces, want.Name)
		assert.Equal(t, want.Ticks, game.Ticks, want.Name)
		assert.Equal(t, want.Over, game.Over, want.Name)
		assert.Equal(t, want.Board, boardRows(game), want.Name)
	}
}

// 规则表必须与浏览器引擎一字不差。踢墙表里很少用到的位置，整局对照未必覆盖得到。
func TestRuleTablesMatchBrowserEngine(t *testing.T) {
	rules := loadGolden(t).Rules
	assert.Equal(t, pieceBoxes[:], rules.PieceBoxes)
	require.Len(t, rules.SpawnCells, len(spawnCells))
	for piece, cells := range spawnCells {
		assert.Equal(t, flattenPairs(cells[:]), rules.SpawnCells[piece], "spawn cells of piece %d", piece)
	}
	for name, tables := range map[string]struct {
		server  *[4][2][5][2]int
		browser [][][]int
	}{"JLSTZ": {&jlstzKicks, rules.JlstzKicks}, "I": {&iKicks, rules.IKicks}} {
		require.Len(t, tables.browser, 4, name)
		for from := range tables.server {
			require.Len(t, tables.browser[from], 2, name)
			for direction := range tables.server[from] {
				assert.Equal(t, flattenPairs(tables.server[from][direction][:]), tables.browser[from][direction], "%s kicks from %d direction %d", name, from, direction)
			}
		}
	}
	assert.Equal(t, gravityTable[:], rules.Gravity)
	assert.Equal(t, lineScores[:], rules.LineScores)
	assert.Equal(t, lockDelay, rules.LockDelay)
	assert.Equal(t, maxLockResets, rules.MaxLockResets)
	assert.Equal(t, linesPerLevel, rules.LinesPerLevel)
	assert.Equal(t, previewSize, rules.PreviewSize)
}

// 顶出之后再多报帧数、多塞操作都不会改变成绩。
func TestReplayIgnoresInputsAfterTopOut(t *testing.T) {
	topOut := loadGolden(t).Games[0]
	require.True(t, topOut.Over)
	inputs, err := DecodeInputs(topOut.Inputs)
	require.NoError(t, err)

	padded := append(inputs, Input{Tick: topOut.Ticks + 5, Action: ActionHardDrop}, Input{Tick: topOut.Ticks + 6, Action: ActionHold})
	game, err := Replay(topOut.Seed, topOut.Ticks+100, padded)
	require.NoError(t, err)
	assert.Equal(t, topOut.Score, game.Score)
	assert.Equal(t, topOut.Lines, game.Lines)
	assert.Equal(t, topOut.Ticks, game.Ticks)
}

// 方块按 7 个一袋发放：每一袋恰好是 7 种方块各一个，任何种子都不会长时间缺某一种。
func TestPiecesComeInSevenBags(t *testing.T) {
	for _, seed := range []uint32{0, 1, 2718281828, math.MaxUint32} {
		game := New(seed)
		sequence := []int{game.piece}
		for len(sequence) < 70 {
			sequence = append(sequence, game.takeNext())
		}
		for start := 0; start < len(sequence); start += 7 {
			seen := map[int]bool{}
			for _, piece := range sequence[start : start+7] {
				seen[piece] = true
			}
			assert.Len(t, seen, 7, "seed %d bag %d", seed, start/7)
		}
	}
}

// 操作记录来自浏览器，结构不对、越界或不是人能按出来的记录一律拒绝，不做部分重放。
func TestReplayRejectsMalformedRecords(t *testing.T) {
	_, err := Replay(1, 10, []Input{{Tick: 0, Action: ActionLeft}, {Tick: 9, Action: ActionHardDrop}})
	require.NoError(t, err)

	burst := make([]Input, 2*10+9)
	for i := range burst {
		burst[i] = Input{Tick: 0, Action: ActionLeft}
	}
	cases := map[string]struct {
		ticks  int
		inputs []Input
	}{
		"no ticks":               {ticks: 0},
		"longer than an hour":    {ticks: MaxTicks + 1},
		"input on unplayed tick": {ticks: 10, inputs: []Input{{Tick: 10, Action: ActionLeft}}},
		"inputs out of order":    {ticks: 10, inputs: []Input{{Tick: 5, Action: ActionLeft}, {Tick: 4, Action: ActionLeft}}},
		"unknown action zero":    {ticks: 10, inputs: []Input{{Tick: 1, Action: 0}}},
		"unknown action eight":   {ticks: 10, inputs: []Input{{Tick: 1, Action: ActionHold + 1}}},
		"too many inputs":        {ticks: 10, inputs: burst},
	}
	for name, c := range cases {
		_, err := Replay(1, c.ticks, c.inputs)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}

	for name, encoded := range map[string][]int{
		"odd length":             {0, 1, 3},
		"negative delta":         {-1, 1},
		"beyond an hour of play": {MaxTicks, 1, 1, 1},
	} {
		_, err := DecodeInputs(encoded)
		assert.ErrorIs(t, err, ErrInvalidReplay, name)
	}
}
