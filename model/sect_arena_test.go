package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Elo：同分对局一场 ±16；赢比自己强得多的对手分多，赢比自己弱得多的对手几乎不加分。
func TestSectArenaEloDelta(t *testing.T) {
	cases := []struct {
		rating, other int
		win           bool
		want          int
	}{
		{1000, 1000, true, 16},
		{1000, 1000, false, -16},
		{1000, 1400, true, 29},
		{1400, 1000, true, 3},
		{1400, 1000, false, -29},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, SectArenaEloDelta(c.rating, c.other, c.win), "%d vs %d win=%v", c.rating, c.other, c.win)
	}
}

// 对手按积分就近各取一半，一边不够时另一边补齐；自己和没设守擂阵容的宗门不列出。
func TestListSectArenaOpponentsBalancesAboveAndBelow(t *testing.T) {
	truncateTables(t)
	rows := []SectArena{
		{UserId: 1, Rating: 1000, Lineup: "[1]"},
		{UserId: 2, Rating: 1010, Lineup: "[1]"},
		{UserId: 3, Rating: 1050, Lineup: "[1]"},
		{UserId: 4, Rating: 1100, Lineup: "[1]"},
		{UserId: 5, Rating: 990, Lineup: "[1]"},
		{UserId: 6, Rating: 995, Lineup: ""},
	}
	require.NoError(t, DB.Create(&rows).Error)
	ids := func(list []SectArena) []int {
		var out []int
		for _, row := range list {
			out = append(out, row.UserId)
		}
		return out
	}

	list, err := ListSectArenaOpponents(1, 1000, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 3, 4, 5}, ids(list), "only one sect below, so the rest come from above")
	list, err = ListSectArenaOpponents(1, 1000, 2)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 5}, ids(list))
}
