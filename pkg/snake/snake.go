// Package snake 是游戏中心贪吃蛇的规则引擎。
//
// 20×20 的场地，不能穿墙。开局蛇长 3、头在 (4,10) 朝右，身体在 (3,10)(2,10)(x 向右、y 向下)。场地里始终有一个食物，
// 放在随机一个空格里；吃到后蛇长加一、得 1 分，并立刻放下一个。撞墙或撞到自己(移动后的头与身体重合；尾巴这一步会移走的
// 那一格不算)结束。游戏按步推进，每一步蛇前进一格，步长 max(70, 150 - 4 × 得分) 毫秒，越吃越快；游戏内时长是所有
// 步长之和。每一步开始前最多转一次向，与当前方向相同或相反的转向不生效。
//
// 这是免费游戏，不花游戏币、没有返还率：成绩是吃到的食物数，达到领奖档位(见 operation_setting 里的贪吃蛇规则)才发奖。
//
// 玩家在浏览器里玩的是同一套规则的 JavaScript 版本(web/classic/src/pages/Games/snake/engine.js)。开局时服务端给出
// 随机种子，浏览器按步记下每一次生效的转向，结束时整份上传；服务端用这里的引擎从同一个种子重放整局，成绩、游戏内时长和
// 奖励都以重放结果为准。两份引擎只要有一处规则不同，玩家看到的成绩就会和结算结果对不上，所以改规则时必须两边一起改，
// 并重新生成 testdata 里的跨端对照记录。引擎只用整数运算，随机数是 mulberry32，两种语言都能逐位一致地实现。
package snake

import "errors"

const (
	Width  = 20
	Height = 20

	StartLength = 3
	startX      = 4
	startY      = 10

	// MaxSteps 是一局的步数上限，同时限定服务端重放一局的开销。
	MaxSteps = 50000

	// 步长(毫秒)从 baseStepMs 起，每得 1 分快 stepMsPerFood，最快 minStepMs。
	baseStepMs    = 150
	stepMsPerFood = 4
	minStepMs     = 70
)

// Direction 是蛇头的朝向，数值是浏览器上传记录里的编码，不能改动。
type Direction int

const (
	Up Direction = iota
	Right
	Down
	Left
)

// NoTurn 表示这一步不转向。
const NoTurn Direction = -1

var (
	stepX = [4]int{0, 1, 0, -1}
	stepY = [4]int{-1, 0, 1, 0}
)

var ErrInvalidReplay = errors.New("invalid snake replay")

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

// Game 是一局贪吃蛇的完整状态。格子编号行优先(y × Width + x)。导出字段只读。
type Game struct {
	rng       mulberry32
	body      []int // 从尾到头
	occupied  [Width * Height]bool
	direction Direction
	food      int // 场地被蛇占满时为 -1

	Score     int
	Steps     int // 已推进的步数，包括结束的那一步
	ElapsedMs int // 游戏内时长：已推进的每一步的步长之和
	Over      bool
}

func New(seed uint32) *Game {
	g := &Game{rng: mulberry32{state: seed}, direction: Right}
	for x := startX - StartLength + 1; x <= startX; x++ {
		cell := startY*Width + x
		g.body = append(g.body, cell)
		g.occupied[cell] = true
	}
	g.placeFood()
	return g
}

// StepMs 是下一步的步长(毫秒)。
func (g *Game) StepMs() int {
	return max(minStepMs, baseStepMs-stepMsPerFood*g.Score)
}

// Length 是蛇的长度。
func (g *Game) Length() int {
	return len(g.body)
}

// placeFood 把食物放在随机一个空格里：空格按行优先编号，用一个随机数选。场地被蛇占满时结束。
func (g *Game) placeFood() {
	free := Width*Height - len(g.body)
	if free == 0 {
		g.food, g.Over = -1, true
		return
	}
	pick := int(g.rng.next() % uint32(free))
	for cell := range g.occupied {
		if g.occupied[cell] {
			continue
		}
		if pick == 0 {
			g.food = cell
			return
		}
		pick--
	}
}

// Step 推进一步：先转向 turn(NoTurn 表示不转，不生效的转向忽略)，再让蛇前进一格。返回转向是否生效。
func (g *Game) Step(turn Direction) bool {
	if g.Over {
		return false
	}
	// 与当前方向相同或相反的转向不生效，蛇不能原地掉头咬到自己的脖子。
	turned := turn >= Up && turn <= Left && turn != g.direction && turn != (g.direction+2)%4
	if turned {
		g.direction = turn
	}
	g.ElapsedMs += g.StepMs()
	g.Steps++
	head := g.body[len(g.body)-1]
	x, y := head%Width+stepX[g.direction], head/Width+stepY[g.direction]
	if x < 0 || x >= Width || y < 0 || y >= Height {
		g.Over = true
		return turned
	}
	next := y*Width + x
	// 没吃到食物时尾巴这一步会移走，头可以进入它空出的格子；食物不会放在蛇身上，吃到食物时头不可能是尾巴那一格。
	tail := g.body[0]
	if g.occupied[next] && next != tail {
		g.Over = true
		return turned
	}
	eating := next == g.food
	if !eating {
		g.occupied[tail] = false
		g.body = g.body[1:]
	}
	g.body = append(g.body, next)
	g.occupied[next] = true
	if eating {
		g.Score++
		g.placeFood()
	}
	return turned
}

// Input 是某一步开始前生效的一次转向。
type Input struct {
	Step      int
	Direction Direction
}

// DecodeInputs 解析浏览器上传的转向记录：数组按 [距上一次转向的步数, 方向] 成对排列，第一次转向的步数从 0 算起。
func DecodeInputs(encoded []int) ([]Input, error) {
	if len(encoded)%2 != 0 {
		return nil, ErrInvalidReplay
	}
	inputs := make([]Input, 0, len(encoded)/2)
	step := 0
	for i := 0; i < len(encoded); i += 2 {
		delta := encoded[i]
		if delta < 0 || delta > MaxSteps {
			return nil, ErrInvalidReplay
		}
		step += delta
		if step > MaxSteps {
			return nil, ErrInvalidReplay
		}
		inputs = append(inputs, Input{Step: step, Direction: Direction(encoded[i+1])})
	}
	return inputs, nil
}

// Replay 从种子开始重放一局。steps 是浏览器推进过的步数，inputs 按步排序、每一步最多一次；游戏提前结束时，
// 之后的步和转向不再生效。转向记录只记生效的转向，与当前方向相同或相反的转向在这里同样不生效。
func Replay(seed uint32, steps int, inputs []Input) (*Game, error) {
	if steps < 0 || steps > MaxSteps || len(inputs) > steps {
		return nil, ErrInvalidReplay
	}
	for i, input := range inputs {
		if input.Step < 0 || input.Step >= steps || (i > 0 && input.Step <= inputs[i-1].Step) ||
			input.Direction < Up || input.Direction > Left {
			return nil, ErrInvalidReplay
		}
	}
	g := New(seed)
	next := 0
	for step := 0; step < steps && !g.Over; step++ {
		turn := NoTurn
		if next < len(inputs) && inputs[next].Step == step {
			turn = inputs[next].Direction
			next++
		}
		g.Step(turn)
	}
	return g, nil
}
