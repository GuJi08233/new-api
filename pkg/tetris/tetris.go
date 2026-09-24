// Package tetris 是游戏中心俄罗斯方块的规则引擎。
//
// 玩家在浏览器里玩的是同一套规则的 JavaScript 版本(web/classic/src/pages/Games/tetris/engine.js)。
// 开局时服务端给出随机种子，浏览器按帧记下每个操作，结束时整份上传；服务端用这里的引擎从同一个
// 种子重放整局，成绩和奖励都以重放结果为准。两份引擎只要有一处规则不同，玩家看到的成绩就会和
// 结算结果对不上，所以改规则时必须两边一起改，并重新生成 testdata 里的跨端对照记录。
//
// 引擎只用整数运算，随机数是 mulberry32，两种语言都能逐位一致地实现。
package tetris

import "errors"

const (
	Width  = 10
	Height = 20

	TicksPerSecond = 60
	// MaxTicks 是一局的游戏内时长上限(1 小时)，到点自动结束，同时限定服务端重放一局的开销。
	MaxTicks = 60 * 60 * TicksPerSecond

	// SpeedWindowTicks 与 HumanPieceLimit 划出人类的操作极限。浏览器的按住重复是 10 帧后每 2 帧一格，
	// 在这套手感下连续 10 秒、每秒锁定 7 块已经快过顶尖玩家，更快的对局只可能是脚本在操作。
	SpeedWindowTicks = 10 * TicksPerSecond
	HumanPieceLimit  = 70

	lockDelay     = 30 // 落地 0.5 秒后锁定
	maxLockResets = 15 // 落地后移动或旋转最多重新计时 15 次，降到更低的行后次数清零
	linesPerLevel = 10
	previewSize   = 5
)

// Action 是玩家的一个操作，数值是浏览器上传记录里的编码，不能改动。
type Action int

const (
	ActionLeft Action = iota + 1
	ActionRight
	ActionSoftDrop
	ActionHardDrop
	ActionRotateCW
	ActionRotateCCW
	ActionHold
)

const (
	pieceI = iota
	pieceO
	pieceT
	pieceS
	pieceZ
	pieceJ
	pieceL
)

var ErrInvalidReplay = errors.New("invalid tetris replay")

// pieceBoxes 是每种方块旋转所在方框的边长，spawnCells 是出生朝向下各格在方框内的坐标(x 向右，y 向下)。
// 其余朝向由方框内顺时针旋转得到，与 SRS 的旋转中心一致。
var pieceBoxes = [7]int{4, 2, 3, 3, 3, 3, 3}

var spawnCells = [7][4][2]int{
	{{0, 1}, {1, 1}, {2, 1}, {3, 1}}, // I
	{{0, 0}, {1, 0}, {0, 1}, {1, 1}}, // O
	{{1, 0}, {0, 1}, {1, 1}, {2, 1}}, // T
	{{1, 0}, {2, 0}, {0, 1}, {1, 1}}, // S
	{{0, 0}, {1, 0}, {1, 1}, {2, 1}}, // Z
	{{0, 0}, {0, 1}, {1, 1}, {2, 1}}, // J
	{{2, 0}, {0, 1}, {1, 1}, {2, 1}}, // L
}

var pieceCells = buildPieceCells()

func buildPieceCells() [7][4][4][2]int {
	var cells [7][4][4][2]int
	for piece := range spawnCells {
		n := pieceBoxes[piece]
		cells[piece][0] = spawnCells[piece]
		for rotation := 1; rotation < 4; rotation++ {
			for i, cell := range cells[piece][rotation-1] {
				cells[piece][rotation][i] = [2]int{n - 1 - cell[1], cell[0]}
			}
		}
	}
	return cells
}

// SRS 踢墙表，照抄规范里的写法(x 向右，y 向上)，使用时把 y 取反。
// 下标为 [起始朝向][0 顺时针 / 1 逆时针]，朝向 0/1/2/3 即规范里的 0/R/2/L。
var jlstzKicks = [4][2][5][2]int{
	{{{0, 0}, {-1, 0}, {-1, 1}, {0, -2}, {-1, -2}}, {{0, 0}, {1, 0}, {1, 1}, {0, -2}, {1, -2}}},
	{{{0, 0}, {1, 0}, {1, -1}, {0, 2}, {1, 2}}, {{0, 0}, {1, 0}, {1, -1}, {0, 2}, {1, 2}}},
	{{{0, 0}, {1, 0}, {1, 1}, {0, -2}, {1, -2}}, {{0, 0}, {-1, 0}, {-1, 1}, {0, -2}, {-1, -2}}},
	{{{0, 0}, {-1, 0}, {-1, -1}, {0, 2}, {-1, 2}}, {{0, 0}, {-1, 0}, {-1, -1}, {0, 2}, {-1, 2}}},
}

var iKicks = [4][2][5][2]int{
	{{{0, 0}, {-2, 0}, {1, 0}, {-2, -1}, {1, 2}}, {{0, 0}, {-1, 0}, {2, 0}, {-1, 2}, {2, -1}}},
	{{{0, 0}, {-1, 0}, {2, 0}, {-1, 2}, {2, -1}}, {{0, 0}, {2, 0}, {-1, 0}, {2, 1}, {-1, -2}}},
	{{{0, 0}, {2, 0}, {-1, 0}, {2, 1}, {-1, -2}}, {{0, 0}, {1, 0}, {-2, 0}, {1, -2}, {-2, 1}}},
	{{{0, 0}, {1, 0}, {-2, 0}, {1, -2}, {-2, 1}}, {{0, 0}, {-2, 0}, {1, 0}, {-2, -1}, {1, 2}}},
}

// gravityTable 是各等级下方块每下落一行所需的帧数，13 级起每帧下落一行。
var gravityTable = [...]int{60, 48, 37, 28, 21, 16, 11, 8, 6, 4, 3, 2, 1}

var lineScores = [5]int{0, 100, 300, 500, 800}

// mulberry32 是一个 32 位伪随机数发生器，JavaScript 用 Math.imul 可以得到逐位相同的序列。
type mulberry32 struct {
	state uint32
}

func (r *mulberry32) next() uint32 {
	r.state += 0x6D2B79F5
	t := r.state
	t = (t ^ (t >> 15)) * (t | 1)
	t = (t + (t^(t>>7))*(t|61)) ^ t
	return t ^ (t >> 14)
}

// Game 是一局俄罗斯方块的完整状态。导出字段只读。
type Game struct {
	board [Height][Width]uint8 // 0 为空，否则为方块种类 + 1
	rng   mulberry32
	queue []int

	piece    int
	x, y     int
	rotation int
	hold     int // -1 表示暂存区为空
	holdUsed bool

	gravityTimer int
	lockTimer    int
	lockResets   int
	lowestY      int

	Score  int
	Lines  int
	Level  int
	Pieces int // 已锁定的方块数
	Ticks  int // 已推进的帧数，包括结束的那一帧
	Over   bool

	// PeakPieces 是任意连续 SpeedWindowTicks 帧内锁定的最多方块数，只由 Replay 统计。
	PeakPieces int
}

func New(seed uint32) *Game {
	g := &Game{rng: mulberry32{state: seed}, hold: -1, Level: 1}
	g.spawn(g.takeNext())
	return g
}

// takeNext 取出下一个方块。方块序列按 7 个一袋洗牌生成，只由种子决定。
func (g *Game) takeNext() int {
	for len(g.queue) <= previewSize {
		bag := [7]int{0, 1, 2, 3, 4, 5, 6}
		for i := 6; i > 0; i-- {
			j := int(g.rng.next() % uint32(i+1))
			bag[i], bag[j] = bag[j], bag[i]
		}
		g.queue = append(g.queue, bag[:]...)
	}
	piece := g.queue[0]
	g.queue = g.queue[1:]
	return piece
}

// spawn 让方块从顶部出现，出生位置被占住即结束。
func (g *Game) spawn(piece int) {
	g.piece, g.rotation = piece, 0
	g.x, g.y = 3, 0
	if piece == pieceO {
		g.x = 4
	}
	g.gravityTimer, g.lockTimer, g.lockResets = 0, 0, 0
	g.lowestY = g.y
	if !g.fits(g.x, g.y, g.rotation) {
		g.Over = true
	}
}

func (g *Game) fits(x, y, rotation int) bool {
	for _, cell := range pieceCells[g.piece][rotation] {
		col, row := x+cell[0], y+cell[1]
		if col < 0 || col >= Width || row < 0 || row >= Height || g.board[row][col] != 0 {
			return false
		}
	}
	return true
}

// Tick 推进一帧：先依次执行这一帧的操作，再结算重力与落地锁定。
func (g *Game) Tick(actions []Action) {
	if g.Over {
		return
	}
	g.Ticks++
	for _, action := range actions {
		g.apply(action)
		if g.Over {
			return
		}
	}
	g.gravityTimer++
	if g.gravityTimer >= gravityTicks(g.Level) {
		g.gravityTimer = 0
		g.moveDown()
	}
	if g.fits(g.x, g.y+1, g.rotation) {
		g.lockTimer = 0
		return
	}
	g.lockTimer++
	if g.lockTimer >= lockDelay {
		g.lock()
	}
}

func gravityTicks(level int) int {
	if level > len(gravityTable) {
		return 1
	}
	return gravityTable[level-1]
}

func (g *Game) apply(action Action) {
	switch action {
	case ActionLeft:
		g.shift(-1)
	case ActionRight:
		g.shift(1)
	case ActionSoftDrop:
		if g.moveDown() {
			g.Score++
			g.gravityTimer = 0
		}
	case ActionHardDrop:
		for g.moveDown() {
			g.Score += 2
		}
		g.lock()
	case ActionRotateCW:
		g.rotate(0)
	case ActionRotateCCW:
		g.rotate(1)
	case ActionHold:
		g.holdPiece()
	}
}

func (g *Game) moveDown() bool {
	if !g.fits(g.x, g.y+1, g.rotation) {
		return false
	}
	g.y++
	if g.y > g.lowestY {
		g.lowestY = g.y
		g.lockResets = 0
	}
	return true
}

func (g *Game) shift(dx int) {
	if g.fits(g.x+dx, g.y, g.rotation) {
		g.x += dx
		g.resetLock()
	}
}

// rotate 按 SRS 依次尝试踢墙位置，direction 为 0 顺时针、1 逆时针。O 方块不旋转。
func (g *Game) rotate(direction int) {
	if g.piece == pieceO {
		return
	}
	target := (g.rotation + 1) % 4
	if direction == 1 {
		target = (g.rotation + 3) % 4
	}
	kicks := &jlstzKicks
	if g.piece == pieceI {
		kicks = &iKicks
	}
	for _, kick := range kicks[g.rotation][direction] {
		x, y := g.x+kick[0], g.y-kick[1]
		if !g.fits(x, y, target) {
			continue
		}
		g.x, g.y, g.rotation = x, y, target
		if g.y > g.lowestY {
			g.lowestY = g.y
			g.lockResets = 0
		}
		g.resetLock()
		return
	}
}

// resetLock 在落地后移动或旋转成功时重新开始锁定计时，次数用完后照常锁定。
func (g *Game) resetLock() {
	if g.lockTimer > 0 && g.lockResets < maxLockResets {
		g.lockTimer = 0
		g.lockResets++
	}
}

// holdPiece 把当前方块放进暂存区，换出暂存的方块(暂存区为空时取下一个)。每落下一个方块只能用一次。
func (g *Game) holdPiece() {
	if g.holdUsed {
		return
	}
	current := g.piece
	if g.hold < 0 {
		g.hold = current
		g.spawn(g.takeNext())
	} else {
		held := g.hold
		g.hold = current
		g.spawn(held)
	}
	g.holdUsed = true
}

func (g *Game) lock() {
	for _, cell := range pieceCells[g.piece][g.rotation] {
		g.board[g.y+cell[1]][g.x+cell[0]] = uint8(g.piece + 1)
	}
	cleared := g.clearLines()
	g.Lines += cleared
	g.Score += lineScores[cleared] * g.Level
	g.Level = 1 + g.Lines/linesPerLevel
	g.Pieces++
	g.holdUsed = false
	g.spawn(g.takeNext())
}

func (g *Game) clearLines() int {
	cleared := 0
	for row := Height - 1; row >= 0; row-- {
		full := true
		for col := 0; col < Width; col++ {
			if g.board[row][col] == 0 {
				full = false
				break
			}
		}
		if full {
			cleared++
			continue
		}
		if cleared > 0 {
			g.board[row+cleared] = g.board[row]
		}
	}
	for row := 0; row < cleared; row++ {
		g.board[row] = [Width]uint8{}
	}
	return cleared
}

// Input 是某一帧上的一个操作。
type Input struct {
	Tick   int
	Action Action
}

// DecodeInputs 解析浏览器上传的操作记录：数组按 [距上一个操作的帧数, 操作] 成对排列，
// 第一个操作的帧数从 0 算起。
func DecodeInputs(encoded []int) ([]Input, error) {
	if len(encoded)%2 != 0 {
		return nil, ErrInvalidReplay
	}
	inputs := make([]Input, 0, len(encoded)/2)
	tick := 0
	for i := 0; i < len(encoded); i += 2 {
		delta := encoded[i]
		if delta < 0 || delta > MaxTicks {
			return nil, ErrInvalidReplay
		}
		tick += delta
		if tick > MaxTicks {
			return nil, ErrInvalidReplay
		}
		inputs = append(inputs, Input{Tick: tick, Action: Action(encoded[i+1])})
	}
	return inputs, nil
}

// Replay 从种子开始重放一局。ticks 是浏览器推进过的帧数，inputs 按帧排序；
// 游戏提前结束时，之后的帧和操作不再生效。平均每帧超过两个操作不是人能按出来的，直接拒绝。
// 重放时顺带统计 PeakPieces，由调用方与 HumanPieceLimit 比较，判断这局是不是脚本打的。
func Replay(seed uint32, ticks int, inputs []Input) (*Game, error) {
	if ticks < 1 || ticks > MaxTicks || len(inputs) > 2*ticks+8 {
		return nil, ErrInvalidReplay
	}
	for i, input := range inputs {
		if input.Tick < 0 || input.Tick >= ticks || (i > 0 && input.Tick < inputs[i-1].Tick) ||
			input.Action < ActionLeft || input.Action > ActionHold {
			return nil, ErrInvalidReplay
		}
	}
	g := New(seed)
	actions := make([]Action, 0, 8)
	// lockTicks 依次记下最近 SpeedWindowTicks 帧内每个方块锁定时的帧号。
	var lockTicks []int
	next := 0
	for tick := 0; tick < ticks && !g.Over; tick++ {
		actions = actions[:0]
		for next < len(inputs) && inputs[next].Tick == tick {
			actions = append(actions, inputs[next].Action)
			next++
		}
		locked := g.Pieces
		g.Tick(actions)
		for ; locked < g.Pieces; locked++ {
			lockTicks = append(lockTicks, tick)
		}
		for len(lockTicks) > 0 && lockTicks[0] <= tick-SpeedWindowTicks {
			lockTicks = lockTicks[1:]
		}
		g.PeakPieces = max(g.PeakPieces, len(lockTicks))
	}
	return g, nil
}
