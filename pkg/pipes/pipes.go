// Package pipes 实现接水管解谜：棋盘中央是水源，每格是一段水管，点击把它转 90 度。所有管口都和相邻管口对上、
// 没有管口朝向棋盘外，并且每一格都和水源连通时过关。盘面由随机生成树打乱方向得到，保证有解；任何满足条件的接法都算过关，
// 不要求与生成时的方向一致。
package pipes

import (
	"errors"
	"math/rand/v2"
)

var ErrMove = errors.New("invalid pipes move")

// 管口方向的位：上、右、下、左。顺时针转 90 度时每一位移到下一个方向。
const (
	Up    = 1
	Right = 2
	Down  = 4
	Left  = 8
)

var directions = [4]int{Up, Right, Down, Left}

// MaxMoves 是一局最多转几下，防止存档无限增长。最大盘 81 格，每格最多转两下就能到任何方向，2000 下绰绰有余。
const MaxMoves = 2000

// MaxSize 是最大的边长，控制器据此挡住越界的格子编号。
const MaxSize = 9

type Level struct {
	Size int `json:"size"`
}

// Levels 按难度编号(1..3)定义边长。
var Levels = [4]Level{
	{},
	{Size: 5},
	{Size: 7},
	{Size: MaxSize},
}

type Game struct {
	Size   int   `json:"size"`
	Source int   `json:"source"`
	Tiles  []int `json:"tiles"`
	// Par 是把每格转回生成时方向所需的最少次数(顺逆时针都算一下)。别的接法可能更少，只用来评星。
	Par int `json:"par"`
}

type treeEdge struct{ from, dir int }

// New 按难度与种子生成一局：从中央水源随机长出一棵覆盖全盘的生成树，每格的管口就是它在树上的边，再把每格随机转若干下。
func New(difficulty int, seed uint64) (*Game, error) {
	if difficulty < 1 || difficulty > 3 {
		return nil, ErrMove
	}
	size := Levels[difficulty].Size
	rng := rand.New(rand.NewPCG(seed, 0x9195))
	solution := spanningTree(size, rng)
	tiles := make([]int, len(solution))
	game := &Game{Size: size, Source: len(solution) / 2, Tiles: tiles}
	for cell, tile := range solution {
		tiles[cell] = rotate(tile, rng.IntN(4))
		game.Par += turnsBack(tile, tiles[cell])
	}
	// 打乱后正好全部接通时(小盘上概率极低)，把第一个转了会变样的格子再转一下，保证开局不是已解。
	if game.Solved() {
		for cell, tile := range tiles {
			if rotate(tile, 1) != tile {
				tiles[cell] = rotate(tile, 1)
				game.Par++
				break
			}
		}
	}
	return game, nil
}

// spanningTree 用随机 Prim 从中央水源长出一棵覆盖 size×size 全盘的树，返回每格在树上的管口。
// 和深度优先相比分叉多、长直管少，盘面更像管网。
func spanningTree(size int, rng *rand.Rand) []int {
	cells := size * size
	source := cells / 2
	tiles := make([]int, cells)
	inTree := make([]bool, cells)
	inTree[source] = true
	frontier := make([]treeEdge, 0, cells*4)
	frontier = appendFrontier(frontier, source, size, inTree)
	for len(frontier) > 0 {
		pick := rng.IntN(len(frontier))
		e := frontier[pick]
		frontier[pick] = frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		next, _ := neighbor(e.from, e.dir, size)
		if inTree[next] {
			continue
		}
		inTree[next] = true
		tiles[e.from] |= e.dir
		tiles[next] |= opposite(e.dir)
		frontier = appendFrontier(frontier, next, size, inTree)
	}
	return tiles
}

// Rotate 把一格转 90 度：clockwise 为真时顺时针，否则逆时针。
func (g *Game) Rotate(cell int, clockwise bool) error {
	if cell < 0 || cell >= len(g.Tiles) {
		return ErrMove
	}
	if clockwise {
		g.Tiles[cell] = rotate(g.Tiles[cell], 1)
	} else {
		g.Tiles[cell] = rotate(g.Tiles[cell], 3)
	}
	return nil
}

// Lit 返回从水源沿着两端对上的管口能流到的格子。
func (g *Game) Lit() []bool {
	lit := make([]bool, len(g.Tiles))
	if g.Source < 0 || g.Source >= len(g.Tiles) {
		return lit
	}
	lit[g.Source] = true
	queue := []int{g.Source}
	for head := 0; head < len(queue); head++ {
		cell := queue[head]
		for _, dir := range directions {
			if g.Tiles[cell]&dir == 0 {
				continue
			}
			next, ok := neighbor(cell, dir, g.Size)
			if !ok || lit[next] || g.Tiles[next]&opposite(dir) == 0 {
				continue
			}
			lit[next] = true
			queue = append(queue, next)
		}
	}
	return lit
}

// Leaks 返回有管口没对上(朝向棋盘外，或相邻格在这个方向没有管口)的格子数。
func (g *Game) Leaks() int {
	leaks := 0
	for cell, tile := range g.Tiles {
		for _, dir := range directions {
			if tile&dir == 0 {
				continue
			}
			next, ok := neighbor(cell, dir, g.Size)
			if !ok || g.Tiles[next]&opposite(dir) == 0 {
				leaks++
				break
			}
		}
	}
	return leaks
}

// Solved 判断是否过关：没有漏水的管口，并且每一格都连到水源。管口总数在转动中不变，恰好是生成树的边数，
// 全部对上又全部连通时一定还是一棵树，不会有环。
func (g *Game) Solved() bool {
	if len(g.Tiles) == 0 || g.Leaks() > 0 {
		return false
	}
	for _, on := range g.Lit() {
		if !on {
			return false
		}
	}
	return true
}

func appendFrontier(frontier []treeEdge, cell, size int, inTree []bool) []treeEdge {
	for _, dir := range directions {
		if next, ok := neighbor(cell, dir, size); ok && !inTree[next] {
			frontier = append(frontier, treeEdge{cell, dir})
		}
	}
	return frontier
}

func neighbor(cell, dir, size int) (int, bool) {
	x, y := cell%size, cell/size
	switch dir {
	case Up:
		y--
	case Right:
		x++
	case Down:
		y++
	case Left:
		x--
	}
	if x < 0 || y < 0 || x >= size || y >= size {
		return 0, false
	}
	return y*size + x, true
}

func opposite(dir int) int {
	return rotate(dir, 2)
}

// rotate 把管口顺时针转 quarter 个 90 度。
func rotate(tile, quarter int) int {
	for i := 0; i < quarter%4; i++ {
		tile = (tile<<1 | tile>>3) & 15
	}
	return tile
}

// turnsBack 返回从 scrambled 最少转几下(顺逆时针都可以)能回到 original 的形状。
func turnsBack(original, scrambled int) int {
	for k := 0; k < 4; k++ {
		if rotate(scrambled, k) == original {
			return min(k, 4-k)
		}
	}
	return 0
}
