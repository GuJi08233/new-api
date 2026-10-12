// Package breakout 使用整数坐标模拟打砖块。浏览器逐帧记录挡板目标位置，服务端按同一种子重放。
package breakout

import "errors"

const (
	Width       = 420
	Height      = 540
	FPS         = 60
	MaxTicks    = 5 * 60 * FPS
	Radius      = 5
	PaddleY     = 490
	Columns     = 8
	Rows        = 6
	BrickWidth  = 44
	BrickHeight = 18
	BrickGap    = 5
	BrickLeft   = 16
	BrickTop    = 64
)

var ErrReplay = errors.New("invalid breakout replay")

type Ball struct{ X, Y, VX, VY int }
type Game struct {
	Bricks []int
	Balls  []Ball
	Paddle int
	Lives  int
	Score  int
	Broken int
	Ticks  int
	Wait   int
	Wide   int
	Over   bool
	Won    bool
}

func New(seed uint32) *Game {
	g := &Game{Bricks: make([]int, Rows*Columns), Paddle: Width / 2, Lives: 3, Wait: 60}
	if seed == 0 {
		seed = 1
	}
	for i := range g.Bricks {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		g.Bricks[i] = 1
		if seed%7 == 0 {
			g.Bricks[i] = 2
		}
	}
	g.Balls = []Ball{{Width / 2, PaddleY - Radius - 1, 3, -4}}
	return g
}

// Step 与浏览器 engine.js 对齐；速度小于砖块厚度，碰撞顺序固定为边界、挡板、按编号的第一块砖。
func (g *Game) Step(target int) {
	if g.Over {
		return
	}
	g.Ticks++
	half := 36
	if g.Wide > 0 {
		half = 56
		g.Wide--
	}
	g.Paddle = max(half, min(Width-half, target))
	if g.Wait > 0 {
		g.Wait--
		g.Balls[0].X = g.Paddle
	} else {
		next := make([]Ball, 0, len(g.Balls))
		var extras []Ball
		for _, ball := range g.Balls {
			oldY := ball.Y
			ball.X += ball.VX
			ball.Y += ball.VY
			if ball.X < Radius {
				ball.X = Radius
				ball.VX = -ball.VX
			}
			if ball.X > Width-Radius {
				ball.X = Width - Radius
				ball.VX = -ball.VX
			}
			if ball.Y < Radius {
				ball.Y = Radius
				ball.VY = -ball.VY
			}
			if ball.VY > 0 && oldY+Radius <= PaddleY && ball.Y+Radius >= PaddleY && ball.X >= g.Paddle-half-Radius && ball.X <= g.Paddle+half+Radius {
				ball.Y = PaddleY - Radius
				ball.VX = (ball.X - g.Paddle) * 4 / half
				ball.VX = max(-4, min(4, ball.VX))
				if ball.VX == 0 {
					ball.VX = 1
				}
				ball.VY = -4
			}
			for i, hp := range g.Bricks {
				if hp == 0 {
					continue
				}
				x, y := BrickLeft+i%Columns*(BrickWidth+BrickGap), BrickTop+i/Columns*(BrickHeight+BrickGap)
				if ball.X+Radius <= x || ball.X-Radius >= x+BrickWidth || ball.Y+Radius <= y || ball.Y-Radius >= y+BrickHeight {
					continue
				}
				if oldY+Radius <= y {
					ball.Y = y - Radius
					ball.VY = -ball.VY
				} else if oldY-Radius >= y+BrickHeight {
					ball.Y = y + BrickHeight + Radius
					ball.VY = -ball.VY
				} else {
					if ball.VX > 0 {
						ball.X = x - Radius
					} else {
						ball.X = x + BrickWidth + Radius
					}
					ball.VX = -ball.VX
				}
				g.Bricks[i]--
				g.Score += 5
				if g.Bricks[i] == 0 {
					g.Broken++
					g.Score += 10
					if i%11 == 0 {
						g.Wide = FPS * 15
					}
					if i%13 == 0 && len(g.Balls)+len(extras) < 3 {
						extras = append(extras, Ball{ball.X, ball.Y, -ball.VX, -4})
					}
				}
				break
			}
			if ball.Y-Radius < Height {
				next = append(next, ball)
			}
		}
		g.Balls = append(next, extras...)
		if g.Broken == Rows*Columns {
			g.Won = true
			g.Over = true
			g.Score += 100
		} else if len(g.Balls) == 0 {
			g.Lives--
			if g.Lives == 0 {
				g.Over = true
			} else {
				g.Wait = 60
				g.Balls = []Ball{{g.Paddle, PaddleY - Radius - 1, 3, -4}}
			}
		}
	}
	if g.Ticks >= MaxTicks {
		g.Over = true
	}
}

func Replay(seed uint32, inputs []int) (*Game, error) {
	if len(inputs) == 0 || len(inputs) > MaxTicks {
		return nil, ErrReplay
	}
	g := New(seed)
	for _, target := range inputs {
		if target < 0 || target > Width {
			return nil, ErrReplay
		}
		g.Step(target)
	}
	return g, nil
}
