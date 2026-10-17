package xiangqi

import "strconv"

var chineseNumbers = [...]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// Notation 返回走前棋盘上一步已记录着法的中文纵线记谱。只检查棋子运动，完整合法性由 Game.Play 保证。
// 红方用中文数字，黑方用阿拉伯数字；
// 相同棋子同路时用前/后消歧，士相仍用路数，因为它们的进退已经能区分。
// 三兵同路用前/中/后；四五兵同路或多路叠兵时，仅给叠兵按己方从右到左、从前到后的顺序编中文序号。
// 该序号黑方也用中文，例如“一卒平5”，见 https://www.xqbase.com/protocol/cchess_move.htm 。
func Notation(board string, m Move) (string, error) {
	if len(board) != Squares {
		return "", ErrInvalidState
	}
	if m.From < 0 || m.From >= Squares || m.To < 0 || m.To >= Squares {
		return "", ErrIllegalMove
	}
	piece, ok := parsePiece(board[m.From])
	if !ok {
		return "", ErrIllegalMove
	}
	pos := Position{turn: piece.Color()}
	for sq := range board {
		if board[sq] == '.' {
			continue
		}
		other, ok := parsePiece(board[sq])
		if !ok {
			return "", ErrInvalidState
		}
		pos.board[sq] = other
	}
	// 只匹配棋子走法，不为每一历史步枚举并试走所有合法应着，也不重新判将。
	var buffer [128]Move
	possible := false
	for _, candidate := range pos.generate(buffer[:0], false) {
		if candidate == m {
			possible = true
			break
		}
	}
	if !possible {
		return "", ErrIllegalMove
	}

	color, kind := piece.Color(), piece.Kind()
	fromX, fromY := SquareXY(m.From)
	toX, toY := SquareXY(m.To)
	names := [...]string{"", "帅", "仕", "相", "马", "车", "炮", "兵"}
	if color == Black {
		names[King], names[Advisor], names[Elephant], names[Pawn] = "将", "士", "象", "卒"
	}
	name := names[kind]
	prefix := name + notationNumber(color, notationFile(color, fromX))
	if kind == Rook || kind == Cannon || kind == Horse || kind == Pawn {
		var onFile [Width]int
		for sq, other := range pos.board {
			if other == piece {
				onFile[sq%Width]++
			}
		}
		if onFile[fromX] > 1 {
			stackedFiles := 0
			for _, count := range onFile {
				if count > 1 {
					stackedFiles++
				}
			}
			ordinal, total := 0, 0
			// 每方第一路在自己的右手边；最前方的棋子最靠近对方底线。
			for file := 1; file <= Width; file++ {
				x := file - 1
				if color == Red {
					x = Width - file
				}
				if onFile[x] < 2 {
					continue
				}
				for rank := 0; rank < Height; rank++ {
					y := rank
					if color == Red {
						y = Height - 1 - rank
					}
					sq := Square(x, y)
					if pos.board[sq] == piece {
						total++
						if sq == m.From {
							ordinal = total
						}
					}
				}
			}
			if (kind != Pawn && total != 2) || (kind == Pawn && total > 5) {
				return "", ErrInvalidState
			}
			label := "前"
			switch {
			case kind == Pawn && (stackedFiles > 1 || total > 3):
				label = chineseNumbers[ordinal]
			case ordinal == total:
				label = "后"
			case ordinal > 1:
				label = "中"
			}
			prefix = label + name
		}
	}

	direction, destination := "平", notationFile(color, toX)
	if toY != fromY {
		distance := (toY - fromY) * forward(color)
		direction = "进"
		if distance < 0 {
			direction, distance = "退", -distance
		}
		if kind != Horse && kind != Advisor && kind != Elephant {
			destination = distance
		}
	}
	return prefix + direction + notationNumber(color, destination), nil
}

func notationFile(color Color, x int) int {
	if color == Red {
		return Width - x
	}
	return x + 1
}

func notationNumber(color Color, n int) string {
	if color == Red {
		return chineseNumbers[n]
	}
	return strconv.Itoa(n)
}
