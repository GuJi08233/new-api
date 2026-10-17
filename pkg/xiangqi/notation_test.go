package xiangqi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotationOpeningFromCompetitionRules(t *testing.T) {
	game := NewGame()
	for _, tc := range []struct {
		move Move
		want string
	}{
		{move(7, 2, 4, 2), "炮二平五"},
		{move(7, 9, 6, 7), "马8进7"},
		{move(7, 0, 6, 2), "马二进三"},
		{move(8, 9, 7, 9), "车9平8"},
		{move(8, 0, 7, 0), "车一平二"},
		{move(7, 7, 7, 3), "炮8进4"},
	} {
		got, err := Notation(game.Board, tc.move)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
		require.NoError(t, game.Play(tc.move))
	}
}

func TestNotationDirectionsAndPieceNames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pieces map[[2]int]byte
		move   Move
		want   string
	}{
		{"red horse retreat uses destination file", map[[2]int]byte{{6, 4}: 'N'}, move(6, 4, 7, 2), "马三退二"},
		{"black horse retreat uses destination file", map[[2]int]byte{{6, 5}: 'n'}, move(6, 5, 7, 7), "马7退8"},
		{"red advisor advance", map[[2]int]byte{{5, 0}: 'A'}, move(5, 0, 4, 1), "仕四进五"},
		{"black advisor retreat", map[[2]int]byte{{4, 8}: 'a'}, move(4, 8, 3, 9), "士5退4"},
		{"red elephant retreat", map[[2]int]byte{{4, 2}: 'B'}, move(4, 2, 6, 0), "相五退三"},
		{"black elephant advance", map[[2]int]byte{{2, 9}: 'b'}, move(2, 9, 4, 7), "象3进5"},
		{"red king advances by distance", nil, move(3, 0, 3, 1), "帅六进一"},
		{"black king retreats by distance", map[[2]int]byte{{5, 9}: '.', {5, 8}: 'k'}, move(5, 8, 5, 9), "将6退1"},
		{"king horizontal uses destination file", nil, move(3, 0, 4, 0), "帅六平五"},
		{"red rook advances by distance", map[[2]int]byte{{2, 3}: 'R'}, move(2, 3, 2, 6), "车七进三"},
		{"black rook advances in opposite direction", map[[2]int]byte{{2, 6}: 'r'}, move(2, 6, 2, 3), "车3进3"},
		{"red rook retreats by distance", map[[2]int]byte{{2, 6}: 'R'}, move(2, 6, 2, 3), "车七退三"},
		{"black rook retreats in opposite direction", map[[2]int]byte{{2, 3}: 'r'}, move(2, 3, 2, 6), "车3退3"},
		{"red cannon capture", map[[2]int]byte{{2, 2}: 'C', {2, 3}: 'P', {2, 9}: 'n'}, move(2, 2, 2, 9), "炮七进七"},
		{"black cannon capture", map[[2]int]byte{{6, 7}: 'c', {6, 6}: 'p', {6, 0}: 'N'}, move(6, 7, 6, 0), "炮7进7"},
		{"red pawn before river", map[[2]int]byte{{6, 3}: 'P'}, move(6, 3, 6, 4), "兵三进一"},
		{"black pawn before river", map[[2]int]byte{{2, 6}: 'p'}, move(2, 6, 2, 5), "卒3进1"},
		{"red pawn crosses files after river", map[[2]int]byte{{6, 6}: 'P'}, move(6, 6, 5, 6), "兵三平四"},
		{"black pawn crosses files after river", map[[2]int]byte{{2, 3}: 'p'}, move(2, 3, 3, 3), "卒3平4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Notation(notationBoard(tc.pieces), tc.move)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNotationDistinguishesPiecesFromBeforeMove(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pieces map[[2]int]byte
		move   Move
		want   string
	}{
		{"red front rook", map[[2]int]byte{{2, 6}: 'R', {2, 3}: 'R'}, move(2, 6, 4, 6), "前车平五"},
		{"red rear rook", map[[2]int]byte{{2, 6}: 'R', {2, 3}: 'R'}, move(2, 3, 4, 3), "后车平五"},
		{"black front rook faces red", map[[2]int]byte{{2, 6}: 'r', {2, 3}: 'r'}, move(2, 3, 4, 3), "前车平5"},
		{"black rear rook", map[[2]int]byte{{2, 6}: 'r', {2, 3}: 'r'}, move(2, 6, 4, 6), "后车平5"},
		{"front cannon", map[[2]int]byte{{2, 6}: 'C', {2, 3}: 'C'}, move(2, 6, 4, 6), "前炮平五"},
		{"rear horse", map[[2]int]byte{{2, 4}: 'N', {2, 2}: 'N'}, move(2, 2, 3, 4), "后马进六"},
		{"front advisor keeps source file", map[[2]int]byte{{5, 0}: 'A', {5, 2}: 'A'}, move(5, 2, 4, 1), "仕四退五"},
		{"rear advisor keeps source file", map[[2]int]byte{{5, 0}: 'A', {5, 2}: 'A'}, move(5, 0, 4, 1), "仕四进五"},
		{"front elephant keeps source file", map[[2]int]byte{{2, 0}: 'B', {2, 4}: 'B'}, move(2, 4, 4, 2), "相七退五"},
		{"rear elephant keeps source file", map[[2]int]byte{{2, 0}: 'B', {2, 4}: 'B'}, move(2, 0, 4, 2), "相七进五"},
		{"black front advisor keeps source file", map[[2]int]byte{{3, 9}: 'a', {3, 7}: 'a'}, move(3, 7, 4, 8), "士4退5"},
		{"black rear elephant keeps source file", map[[2]int]byte{{6, 9}: 'b', {6, 5}: 'b'}, move(6, 9, 4, 7), "象7进5"},
		{"two pawns use front", map[[2]int]byte{{2, 5}: 'P', {2, 7}: 'P'}, move(2, 7, 3, 7), "前兵平六"},
		{"three pawns use middle", map[[2]int]byte{{2, 5}: 'P', {2, 6}: 'P', {2, 7}: 'P'}, move(2, 6, 3, 6), "中兵平六"},
		{"three black pawns use rear", map[[2]int]byte{{2, 2}: 'p', {2, 3}: 'p', {2, 4}: 'p'}, move(2, 4, 3, 4), "后卒平4"},
		{"four pawns use ordinal", map[[2]int]byte{{2, 5}: 'P', {2, 6}: 'P', {2, 7}: 'P', {2, 8}: 'P'}, move(2, 7, 3, 7), "二兵平六"},
		{"black pawn ordinal remains Chinese", map[[2]int]byte{{2, 1}: 'p', {2, 2}: 'p', {2, 3}: 'p', {2, 4}: 'p'}, move(2, 3, 3, 3), "三卒平4"},
		{"five pawns include last rank", map[[2]int]byte{{2, 5}: 'P', {2, 6}: 'P', {2, 7}: 'P', {2, 8}: 'P', {2, 9}: 'P'}, move(2, 9, 3, 9), "一兵平六"},
		{"multiple pawn files counted from own right", map[[2]int]byte{{6, 6}: 'P', {6, 8}: 'P', {2, 6}: 'P', {2, 8}: 'P', {4, 7}: 'P'}, move(2, 8, 3, 8), "三兵平六"},
		{"single pawn excluded from ordinals", map[[2]int]byte{{6, 6}: 'P', {6, 8}: 'P', {2, 6}: 'P', {2, 8}: 'P', {4, 7}: 'P'}, move(4, 7, 4, 8), "兵五进一"},
		{"black pawn files reverse red order", map[[2]int]byte{{2, 1}: 'p', {2, 3}: 'p', {6, 1}: 'p', {6, 3}: 'p'}, move(6, 1, 7, 1), "三卒平8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Notation(notationBoard(tc.pieces), tc.move)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNotationRejectsUnknownOrIllegalMoves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		board string
		move  Move
	}{
		{"missing board", "", move(7, 2, 4, 2)},
		{"off board", StartBoard, Move{From: -1, To: Squares}},
		{"empty source", StartBoard, move(4, 4, 4, 5)},
		{"blocked horse", StartBoard, move(1, 0, 3, 1)},
		{"pawn retreat", StartBoard, move(0, 3, 0, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Notation(tc.board, tc.move)
			require.Error(t, err)
			assert.Empty(t, got)
		})
	}
}

func notationBoard(pieces map[[2]int]byte) string {
	board := []byte(strings.Repeat(".", Squares))
	board[Square(3, 0)], board[Square(5, 9)] = 'K', 'k'
	for xy, piece := range pieces {
		board[Square(xy[0], xy[1])] = piece
	}
	return string(board)
}
