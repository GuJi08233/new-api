// Package flappy 是游戏中心像素鸟的规则引擎。
//
// 逻辑画布 288×512，地面高 112，可玩区域是 y ∈ [0, 400)，每秒 60 帧。坐标全部是整数，精度放大 100 倍(1 像素 = 100 单位)。
// 鸟的碰撞盒 34×24，左沿固定在 x = 80；开局悬停在可玩区域正中，直到第一次拍翅膀才开始下落，计时也从这一帧开始。
// 每帧重力让下落速度加 25 单位(0.25 像素)，最大下落速度 1000 单位；拍翅膀把速度设为 -460 单位(向上 4.6 像素/帧)。
// 管道宽 52、上下管道之间的缺口高 120，相邻管道间距 180，每帧左移 2 像素；第一根管道的左沿从 x = 400 开始，缺口上沿在
// [60, 220] 像素之间随机取整数。鸟完全越过一根管道(管道右沿在鸟左沿的左边)得 1 分；碰到管道、地面或飞出顶部结束。
//
// 这是免费游戏，不花游戏币、没有返还率：成绩是穿过的管道数，达到领奖档位(见 operation_setting 里的像素鸟规则)才发奖。
// 任意 1 秒内拍翅膀超过 12 次不是人能按出来的，由调用方判定为脚本、不发奖励。
//
// 玩家在浏览器里玩的是同一套规则的 JavaScript 版本(web/classic/src/pages/Games/flappy/engine.js)。开局时服务端给出
// 随机种子，浏览器按帧记下每次拍翅膀，结束时整份上传；服务端用这里的引擎从同一个种子重放整局，成绩和奖励都以重放结果为准。
// 两份引擎只要有一处规则不同，玩家看到的成绩就会和结算结果对不上，所以改规则时必须两边一起改，并重新生成 testdata 里的
// 跨端对照记录。引擎只用整数运算，随机数是 mulberry32，两种语言都能逐位一致地实现。
package flappy

import "errors"

// 尺寸与距离以像素为单位，状态里的坐标与速度以单位(像素 × Scale)计。
const (
	TicksPerSecond = 60
	// MaxTicks 是一局的游戏内时长上限(1 小时)，同时限定服务端重放一局的开销。
	MaxTicks = 60 * 60 * TicksPerSecond

	Scale       = 100
	ScreenWidth = 288
	GroundY     = 400 // 地面上沿，可玩区域的下沿

	BirdX      = 80
	BirdWidth  = 34
	BirdHeight = 24
	BirdStartY = (GroundY - BirdHeight) / 2 // 鸟的上沿，开局悬停在可玩区域正中

	Gravity      = 25   // 单位/帧²
	FlapVelocity = -460 // 单位/帧
	MaxFallSpeed = 1000 // 单位/帧

	PipeWidth   = 52
	PipeGap     = 120
	PipeSpacing = 180
	PipeSpeed   = 2 // 像素/帧
	FirstPipeX  = 400
	GapMinTop   = 60
	GapMaxTop   = GroundY - PipeGap - 60

	// FlapWindowTicks 与 HumanFlapLimit 划出人类的操作极限：任意 1 秒内拍翅膀超过 12 次只可能是脚本。
	FlapWindowTicks = TicksPerSecond
	HumanFlapLimit  = 12
)

var ErrInvalidReplay = errors.New("invalid flappy replay")

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

// Pipe 是一对上下管道：X 是左沿，GapTop 是缺口上沿，单位都是像素 × Scale。
type Pipe struct {
	X      int
	GapTop int
	Passed bool
}

// Game 是一局像素鸟的完整状态。导出字段只读。
type Game struct {
	rng   mulberry32
	pipes []Pipe // 从左到右

	Y  int // 鸟的上沿
	VY int // 向下为正

	Score int
	Ticks int // 已推进的帧数，从第一次拍翅膀那一帧算起，包括结束的那一帧
	Flaps int
	Over  bool

	// PeakFlaps 是任意连续 FlapWindowTicks 帧内拍翅膀的最多次数，只由 Replay 统计。
	PeakFlaps int
}

func New(seed uint32) *Game {
	g := &Game{rng: mulberry32{state: seed}, Y: BirdStartY * Scale}
	g.addPipe(FirstPipeX * Scale)
	return g
}

// addPipe 在 x 处放一对管道，缺口上沿在 [GapMinTop, GapMaxTop] 像素之间，用一个随机数取整数像素。
func (g *Game) addPipe(x int) {
	top := GapMinTop + int(g.rng.next()%uint32(GapMaxTop-GapMinTop+1))
	g.pipes = append(g.pipes, Pipe{X: x, GapTop: top * Scale})
}

// Tick 推进一帧，flap 表示这一帧拍了翅膀。第一次拍翅膀之前鸟悬停、管道不动，帧数也不增加。
// 每一帧依次是：拍翅膀或重力改变速度、鸟移动、管道左移(移出画面的删掉，最后一根进入画面后在它后面补一根)、计分、碰撞。
func (g *Game) Tick(flap bool) {
	if g.Over || (g.Ticks == 0 && !flap) {
		return
	}
	g.Ticks++
	if flap {
		g.VY = FlapVelocity
		g.Flaps++
	} else {
		g.VY = min(g.VY+Gravity, MaxFallSpeed)
	}
	g.Y += g.VY

	for i := range g.pipes {
		g.pipes[i].X -= PipeSpeed * Scale
	}
	for len(g.pipes) > 0 && g.pipes[0].X+PipeWidth*Scale < 0 {
		g.pipes = g.pipes[1:]
	}
	if last := g.pipes[len(g.pipes)-1]; last.X < ScreenWidth*Scale {
		g.addPipe(last.X + PipeSpacing*Scale)
	}
	for i := range g.pipes {
		if !g.pipes[i].Passed && g.pipes[i].X+PipeWidth*Scale < BirdX*Scale {
			g.pipes[i].Passed = true
			g.Score++
		}
	}

	if g.Y < 0 || g.Y+BirdHeight*Scale >= GroundY*Scale {
		g.Over = true
		return
	}
	for _, pipe := range g.pipes {
		overlapX := pipe.X < (BirdX+BirdWidth)*Scale && BirdX*Scale < pipe.X+PipeWidth*Scale
		inGap := g.Y >= pipe.GapTop && g.Y+BirdHeight*Scale <= pipe.GapTop+PipeGap*Scale
		if overlapX && !inGap {
			g.Over = true
			return
		}
	}
}

// DecodeFlaps 解析浏览器上传的拍翅膀记录：每个数是距上一次拍翅膀的帧数，第一次从第 0 帧算起。
func DecodeFlaps(encoded []int) ([]int, error) {
	flaps := make([]int, 0, len(encoded))
	tick := 0
	for _, delta := range encoded {
		if delta < 0 || delta > MaxTicks {
			return nil, ErrInvalidReplay
		}
		tick += delta
		if tick > MaxTicks {
			return nil, ErrInvalidReplay
		}
		flaps = append(flaps, tick)
	}
	return flaps, nil
}

// Replay 从种子开始重放一局。ticks 是浏览器推进过的帧数(从第一次拍翅膀算起)，flaps 是拍翅膀的帧号，严格递增、
// 一帧最多一次；推进过帧的一局第 0 帧一定拍了翅膀。鸟撞上之后，之后的帧和拍翅膀不再生效。
// 重放时顺带统计 PeakFlaps，由调用方与 HumanFlapLimit 比较，判断这局是不是脚本打的。
func Replay(seed uint32, ticks int, flaps []int) (*Game, error) {
	if ticks < 0 || ticks > MaxTicks || len(flaps) > ticks {
		return nil, ErrInvalidReplay
	}
	if ticks > 0 && (len(flaps) == 0 || flaps[0] != 0) {
		return nil, ErrInvalidReplay
	}
	for i, tick := range flaps {
		if tick < 0 || tick >= ticks || (i > 0 && tick <= flaps[i-1]) {
			return nil, ErrInvalidReplay
		}
	}
	g := New(seed)
	next := 0
	for tick := 0; tick < ticks && !g.Over; tick++ {
		flap := next < len(flaps) && flaps[next] == tick
		if flap {
			next++
		}
		g.Tick(flap)
	}
	g.PeakFlaps = peakFlaps(flaps[:next])
	return g, nil
}

// peakFlaps 返回任意连续 FlapWindowTicks 帧内拍翅膀的最多次数。flaps 是严格递增的帧号，只含鸟还活着时拍的那些：
// 相隔不到 FlapWindowTicks 帧的两次算在同一个窗口里，恰好相隔这么多帧就不算。
func peakFlaps(flaps []int) int {
	peak, start := 0, 0
	for end, tick := range flaps {
		for flaps[start] <= tick-FlapWindowTicks {
			start++
		}
		peak = max(peak, end-start+1)
	}
	return peak
}
