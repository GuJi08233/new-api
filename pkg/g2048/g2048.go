// Package g2048 是游戏中心 2048 的规则引擎。
//
// 4×4 的盘面，开局在随机空格里放两个方块。每次让盘面发生变化的移动之后，在随机一个空格里生成一个新方块：
// 九成是 2、一成是 4；没有让盘面变化的移动不生效，也不生成方块。移动时每一行(列)里相邻的相同方块从移动方向那一侧
// 开始两两合并，一次移动里每个方块最多合并一次；得分是所有合并产生的新方块之和。四个方向都移动不了时结束，
// 玩家也可以随时结束并结算。
//
// 这是免费游戏，不花游戏币、没有返还率：成绩就是得分，达到领奖档位(见 operation_setting 里的 2048 规则)才发奖。
//
// 玩家在浏览器里玩的是同一套规则的 JavaScript 版本(web/classic/src/pages/Games/g2048/engine.js)。开局时服务端给出
// 随机种子，浏览器记下每一次移动，结束时整份上传；服务端用这里的引擎从同一个种子重放整局，成绩和奖励都以重放结果为准。
// 两份引擎只要有一处规则不同，玩家看到的成绩就会和结算结果对不上，所以改规则时必须两边一起改，并重新生成 testdata 里的
// 跨端对照记录。引擎只用整数运算，随机数是 mulberry32，两种语言都能逐位一致地实现。
package g2048

import "errors"

const (
	Size  = 4
	cells = Size * Size

	// MaxMoves 是一局移动记录的长度上限，同时限定服务端重放一局的开销。合成 65536 也只要三万多次移动。
	MaxMoves = 50000
	// MaxMovesPerSecond 是人能按出来的移动速度上限：人一秒最多按十次左右，再留一点余量。2048 没有游戏内时钟，
	// 调用方按开局以来的真实时间检查有效移动数。
	MaxMovesPerSecond = 12

	// fourChance 决定新方块的数值：随机数除以它余 0 时是 4，否则是 2。
	fourChance = 10
)

// Direction 是移动方向，数值是浏览器上传记录里的编码('0' 到 '3')，不能改动。
type Direction int

const (
	Up Direction = iota
	Right
	Down
	Left
)

var ErrInvalidReplay = errors.New("invalid 2048 replay")

// lines 是每个移动方向下的四行(列)，每行按从移动方向那一侧往回的顺序列出格子编号(行优先，row × Size + col)。
var lines = buildLines()

func buildLines() [4][Size][Size]int {
	var result [4][Size][Size]int
	for i := 0; i < Size; i++ {
		for j := 0; j < Size; j++ {
			result[Up][i][j] = j*Size + i               // 第 i 列，从上往下
			result[Right][i][j] = i*Size + Size - 1 - j // 第 i 行，从右往左
			result[Down][i][j] = (Size-1-j)*Size + i    // 第 i 列，从下往上
			result[Left][i][j] = i*Size + j             // 第 i 行，从左往右
		}
	}
	return result
}

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

// Game 是一局 2048 的完整状态。导出字段只读。
type Game struct {
	board [cells]int // 行优先，0 为空，否则为方块的数值
	rng   mulberry32

	Score int
	Moves int // 让盘面发生变化的移动数
	Over  bool
}

func New(seed uint32) *Game {
	g := &Game{rng: mulberry32{state: seed}}
	g.spawn()
	g.spawn()
	return g
}

// spawn 在随机一个空格里放一个新方块：空格按行优先编号，先用一个随机数选格子，再用一个随机数决定是 2 还是 4。
func (g *Game) spawn() {
	var empty [cells]int
	count := 0
	for index, value := range g.board {
		if value == 0 {
			empty[count] = index
			count++
		}
	}
	// 让盘面变化的移动一定空出了格子，这里只是兜底。
	if count == 0 {
		return
	}
	index := empty[g.rng.next()%uint32(count)]
	value := 2
	if g.rng.next()%fourChance == 0 {
		value = 4
	}
	g.board[index] = value
}

// Move 向 direction 移动。盘面没有变化(或已经结束)时什么都不做、不生成方块，返回 false。
func (g *Game) Move(direction Direction) bool {
	if g.Over || direction < Up || direction > Left {
		return false
	}
	var next [cells]int
	gained := 0
	for _, line := range lines[direction] {
		// pending 是这一行里还在等合并对象的方块，target 是下一个要填的位置。
		target, pending := 0, 0
		for _, index := range line {
			value := g.board[index]
			if value == 0 {
				continue
			}
			if value == pending {
				next[line[target]] = value * 2
				gained += value * 2
				target++
				pending = 0
				continue
			}
			if pending != 0 {
				next[line[target]] = pending
				target++
			}
			pending = value
		}
		if pending != 0 {
			next[line[target]] = pending
		}
	}
	if next == g.board {
		return false
	}
	g.board = next
	g.Score += gained
	g.Moves++
	g.spawn()
	g.Over = !g.canMove()
	return true
}

// canMove 表示还有让盘面变化的移动：有空格，或者有横向、纵向相邻的相同方块。
func (g *Game) canMove() bool {
	for index, value := range g.board {
		if value == 0 {
			return true
		}
		if index%Size < Size-1 && g.board[index+1] == value {
			return true
		}
		if index+Size < cells && g.board[index+Size] == value {
			return true
		}
	}
	return false
}

// MaxTile 返回盘面上最大的方块。
func (g *Game) MaxTile() int {
	best := 0
	for _, value := range g.board {
		best = max(best, value)
	}
	return best
}

// Replay 从种子开始重放一局。moves 每个字符是一次移动('0' 上、'1' 右、'2' 下、'3' 左)。浏览器只需记下生效的移动，
// 没有让盘面变化的移动在这里同样不生效，结束之后的移动也不再生效。记录过长或含有别的字符时整局拒绝，不做部分重放。
func Replay(seed uint32, moves string) (*Game, error) {
	if len(moves) > MaxMoves {
		return nil, ErrInvalidReplay
	}
	for i := 0; i < len(moves); i++ {
		if moves[i] < '0' || moves[i] > '3' {
			return nil, ErrInvalidReplay
		}
	}
	g := New(seed)
	for i := 0; i < len(moves) && !g.Over; i++ {
		g.Move(Direction(moves[i] - '0'))
	}
	return g, nil
}
