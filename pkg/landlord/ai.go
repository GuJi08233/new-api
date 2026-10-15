package landlord

import (
	"errors"
	"math/rand/v2"
	"slices"
	"sort"
)

// 斗地主 AI，只看 Knowledge 里的信息(自己的手牌、各家剩余张数、叫分、底牌、已经打出的牌与这一轮的出牌)，不看别人的手牌。
//
// 手牌先"拆牌"：拿出王炸和炸弹，再试几种拆法(要不要顺子、连对、飞机)，剩下三张、对子与单张，三张和飞机带上最小的单牌或
// 对子，取出牌手数最少的一种。叫分看大牌(王、2、炸弹)与手数；领出时出拆好的一组，先出小的、多张的，2 和王留着收尾；跟牌时
// 在能压过的牌里挑最不拆坏手牌、点数最小的一手，代价太大就不出，对手快出完时才用炸弹。农民不压队友的牌，队友是下家且只剩
// 一两张时送小牌；地主是下家且快出完时，用大牌顶住队友的小牌。
//
// 三档难度：
//   - 1 简单：随手叫分，领出随机挑一组，跟牌随机挑一手能压过的牌或不出；
//   - 2 普通：按上面的规则出牌，只记得自己的牌；
//   - 3 困难：另外记住打出过的牌，知道哪些牌已经没人压得住：残局先出别人压不住的牌、最后一手出完，对手只剩一张时不送单牌。

var ErrInvalidLevel = errors.New("landlord: invalid ai level")

// MaxHints 限制一次提示的备选数量，前端可以按返回顺序循环选牌。
const MaxHints = 12

// Move 是一步：叫分阶段是 Bid，出牌阶段是 Cards 或 Pass。
type Move struct {
	Bid   *int
	Cards []int
	Pass  bool
}

// counts 按点数记张数。
type counts [RankBigJoker + 1]int

func countRanks(cards []int) counts {
	var c counts
	for _, card := range cards {
		c[Rank(card)]++
	}
	return c
}

func (c counts) total() int {
	total := 0
	for _, n := range c {
		total += n
	}
	return total
}

func (c counts) without(ranks []int) counts {
	for _, rank := range ranks {
		c[rank]--
	}
	return c
}

// group 是按点数记的一手牌，ranks 是它用到的全部点数(带重复，含带的牌)。
type group struct {
	Combination
	ranks []int
}

func rankGroup(kind string, rank int, chain int, ranks ...int) group {
	return group{Combination: Combination{Type: kind, Rank: rank, Chain: chain}, ranks: ranks}
}

// isControl 表示这是一手 2 或王的单张、对子，留着收尾、抢出牌权用。
func (g group) isControl() bool {
	return (g.Type == Single || g.Type == Pair) && g.Rank >= RankTwo
}

func (g group) isBomb() bool {
	return g.Type == Bomb || g.Type == Rocket
}

// plan 是一种拆法。
type plan []group

// cost 衡量一手牌有多难出完：手数为主，小单张最难出，其次是小对子。
func (p plan) cost() int {
	cost := len(p) * 100
	for _, g := range p {
		switch {
		case g.Type == Single && g.Rank < RankAce:
			cost += 4 + RankAce - g.Rank
		case g.Type == Pair && g.Rank < 10:
			cost += 2
		}
	}
	return cost
}

// 拆法：不拆顺子；顺子至少吸收 3 张单牌才拆、2 张就拆；先拆连对再拆顺子。
const (
	planNoChains = iota
	planStrictChains
	planLooseChains
	planPairChainsFirst
	planModes
)

// planHand 试几种拆法，取最好出完的一种。
func planHand(c counts) plan {
	var best plan
	bestCost := -1
	for mode := 0; mode < planModes; mode++ {
		p := buildPlan(c, mode)
		if cost := p.cost(); bestCost < 0 || cost < bestCost {
			best, bestCost = p, cost
		}
	}
	return best
}

func buildPlan(c counts, mode int) plan {
	var p plan
	if c[RankSmallJoker] == 1 && c[RankBigJoker] == 1 {
		p = append(p, rankGroup(Rocket, RankBigJoker, 1, RankSmallJoker, RankBigJoker))
		c[RankSmallJoker], c[RankBigJoker] = 0, 0
	}
	for rank := 3; rank <= RankTwo; rank++ {
		if c[rank] == 4 {
			p = append(p, rankGroup(Bomb, rank, 1, rank, rank, rank, rank))
			c[rank] = 0
		}
	}
	var airplanes []group
	if mode != planNoChains {
		airplanes = takeRuns(&c, 3, 2)
		switch mode {
		case planPairChainsFirst:
			p = append(p, takeRuns(&c, 2, 3)...)
			p = append(p, takeStraights(&c, 2)...)
		case planLooseChains:
			p = append(p, takeStraights(&c, 2)...)
			p = append(p, takeRuns(&c, 2, 3)...)
		default:
			p = append(p, takeStraights(&c, 3)...)
			p = append(p, takeRuns(&c, 2, 3)...)
		}
	}
	var triples []group
	var pairs, singles []int
	for rank := 3; rank <= RankBigJoker; rank++ {
		switch c[rank] {
		case 3:
			triples = append(triples, rankGroup(Triple, rank, 1, rank, rank, rank))
		case 2:
			pairs = append(pairs, rank)
		case 1:
			singles = append(singles, rank)
		}
	}
	// 带牌：飞机先带(要的翅膀多)，三张从小到大各带一张最小的单牌，没有单牌带最小的对子。2 和王留着当大牌，不拿去带。
	used := map[int]bool{}
	take := func(from []int, n int) []int {
		var picked []int
		for _, rank := range from {
			if len(picked) == n {
				break
			}
			if rank < RankTwo && !used[rank] {
				picked = append(picked, rank)
			}
		}
		if len(picked) < n {
			return nil
		}
		for _, rank := range picked {
			used[rank] = true
		}
		return picked
	}
	attach := func(g *group, single string, pair string) {
		if wings := take(singles, g.Chain); wings != nil {
			g.Type = single
			g.ranks = append(g.ranks, wings...)
		} else if wings := take(pairs, g.Chain); wings != nil {
			g.Type = pair
			for _, rank := range wings {
				g.ranks = append(g.ranks, rank, rank)
			}
		}
	}
	for i := range airplanes {
		attach(&airplanes[i], AirplaneSingle, AirplanePair)
	}
	for i := range triples {
		attach(&triples[i], TripleSingle, TriplePair)
	}
	p = append(p, airplanes...)
	p = append(p, triples...)
	for _, rank := range pairs {
		if !used[rank] {
			p = append(p, rankGroup(Pair, rank, 1, rank, rank))
		}
	}
	for _, rank := range singles {
		if !used[rank] {
			p = append(p, rankGroup(Single, rank, 1, rank))
		}
	}
	return p
}

// takeRuns 拿出连续点数里张数正好是 width 的最长序列(3 到 A，至少 minChain 组)：width 3 是飞机，2 是连对。
func takeRuns(c *counts, width int, minChain int) []group {
	var groups []group
	for start := 3; start <= RankAce; {
		end := start
		for end <= RankAce && c[end] == width {
			end++
		}
		if end-start >= minChain {
			kind := PairStraight
			if width == 3 {
				kind = Airplane
			}
			g := rankGroup(kind, end-1, end-start)
			for rank := start; rank < end; rank++ {
				for i := 0; i < width; i++ {
					g.ranks = append(g.ranks, rank)
				}
				c[rank] = 0
			}
			groups = append(groups, g)
		}
		start = max(end, start+1)
	}
	return groups
}

// takeStraights 拆顺子：从小到大找吸收至少 minSingles 张单牌的 5 张顺子，再往两头接单牌，首尾相接的合成一个，
// 一模一样的两个合成连对。
func takeStraights(c *counts, minSingles int) []group {
	var spans [][2]int
	for {
		found := false
		for start := 3; start+4 <= RankAce && !found; start++ {
			singles := 0
			for rank := start; rank < start+5; rank++ {
				if c[rank] == 0 {
					singles = -1
					break
				}
				if c[rank] == 1 {
					singles++
				}
			}
			if singles >= minSingles {
				for rank := start; rank < start+5; rank++ {
					c[rank]--
				}
				spans = append(spans, [2]int{start, start + 4})
				found = true
			}
		}
		if !found {
			break
		}
	}
	for i := range spans {
		for spans[i][1] < RankAce && c[spans[i][1]+1] == 1 {
			spans[i][1]++
			c[spans[i][1]]--
		}
		for spans[i][0] > 3 && c[spans[i][0]-1] == 1 {
			spans[i][0]--
			c[spans[i][0]]--
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var groups []group
	for i := 0; i < len(spans); i++ {
		span := spans[i]
		if i+1 < len(spans) && spans[i+1] == span {
			g := rankGroup(PairStraight, span[1], span[1]-span[0]+1)
			for rank := span[0]; rank <= span[1]; rank++ {
				g.ranks = append(g.ranks, rank, rank)
			}
			groups = append(groups, g)
			i++
			continue
		}
		for i+1 < len(spans) && spans[i+1][0] == span[1]+1 {
			span[1] = spans[i+1][1]
			i++
		}
		g := rankGroup(Straight, span[1], span[1]-span[0]+1)
		for rank := span[0]; rank <= span[1]; rank++ {
			g.ranks = append(g.ranks, rank)
		}
		groups = append(groups, g)
	}
	return groups
}

// kickers 挑 n 个点数带出去(pair 为真时每个点数带一对)，不用 exclude 里的点数。先挑本来就是单张(或对子)的小牌，再拆小对子
// (或小三张)，2 与王放到最后，不拆炸弹。挑不够时返回 false。
func kickers(c counts, exclude []int, n int, pair bool) ([]int, bool) {
	var tiers [][2]int // {张数, 是否大牌}
	if pair {
		tiers = [][2]int{{2, 0}, {3, 0}, {2, 1}, {3, 1}}
	} else {
		tiers = [][2]int{{1, 0}, {2, 0}, {3, 0}, {1, 1}, {2, 1}, {3, 1}}
	}
	var picked []int
	for _, tier := range tiers {
		for rank := 3; rank <= RankBigJoker && len(picked) < n; rank++ {
			big := 0
			if rank >= RankTwo {
				big = 1
			}
			if c[rank] != tier[0] || big != tier[1] || slices.Contains(exclude, rank) || slices.Contains(picked, rank) {
				continue
			}
			picked = append(picked, rank)
		}
	}
	// 飞机带单牌、四带二允许带一对；不同点数不够时再从已挑的点数里补第二张。
	if !pair && len(picked) < n {
		for _, rank := range slices.Clone(picked) {
			if c[rank] >= 2 && len(picked) < n {
				picked = append(picked, rank)
			}
		}
	}
	if len(picked) < n {
		return nil, false
	}
	var ranks []int
	for _, rank := range picked {
		ranks = append(ranks, rank)
		if pair {
			ranks = append(ranks, rank)
		}
	}
	return ranks, true
}

// beatingGroups 列出 c 里能压过 previous 的每一种出法(带的牌按 kickers 挑)，炸弹与王炸排在最后。
func beatingGroups(c counts, previous Combination) []group {
	var out []group
	chain := previous.Chain
	switch previous.Type {
	case Single:
		for rank := previous.Rank + 1; rank <= RankBigJoker; rank++ {
			if c[rank] >= 1 {
				out = append(out, rankGroup(Single, rank, 1, rank))
			}
		}
	case Pair:
		for rank := previous.Rank + 1; rank <= RankTwo; rank++ {
			if c[rank] >= 2 {
				out = append(out, rankGroup(Pair, rank, 1, rank, rank))
			}
		}
	case Triple, TripleSingle, TriplePair:
		for rank := previous.Rank + 1; rank <= RankTwo; rank++ {
			if c[rank] < 3 {
				continue
			}
			g := rankGroup(previous.Type, rank, 1, rank, rank, rank)
			if previous.Type != Triple {
				wings, ok := kickers(c, []int{rank}, 1, previous.Type == TriplePair)
				if !ok {
					continue
				}
				g.ranks = append(g.ranks, wings...)
			}
			out = append(out, g)
		}
	case Straight, PairStraight, Airplane, AirplaneSingle, AirplanePair:
		width := 3
		switch previous.Type {
		case Straight:
			width = 1
		case PairStraight:
			width = 2
		}
		for end := previous.Rank + 1; end <= RankAce; end++ {
			start := end - chain + 1
			if start < 3 {
				continue
			}
			body := make([]int, 0, chain)
			for rank := start; rank <= end && c[rank] >= width; rank++ {
				body = append(body, rank)
			}
			if len(body) != chain {
				continue
			}
			g := rankGroup(previous.Type, end, chain)
			for _, rank := range body {
				for i := 0; i < width; i++ {
					g.ranks = append(g.ranks, rank)
				}
			}
			if previous.Type == AirplaneSingle || previous.Type == AirplanePair {
				wings, ok := kickers(c, body, chain, previous.Type == AirplanePair)
				if !ok {
					continue
				}
				g.ranks = append(g.ranks, wings...)
			}
			out = append(out, g)
		}
	case FourTwo, FourTwoPairs:
		for rank := previous.Rank + 1; rank <= RankTwo; rank++ {
			if c[rank] != 4 {
				continue
			}
			wings, ok := kickers(c, []int{rank}, 2, previous.Type == FourTwoPairs)
			if ok {
				out = append(out, rankGroup(previous.Type, rank, 1, append([]int{rank, rank, rank, rank}, wings...)...))
			}
		}
	}
	if previous.Type == Rocket {
		return out
	}
	for rank := 3; rank <= RankTwo; rank++ {
		if c[rank] == 4 && (previous.Type != Bomb || rank > previous.Rank) {
			out = append(out, rankGroup(Bomb, rank, 1, rank, rank, rank, rank))
		}
	}
	if c[RankSmallJoker] == 1 && c[RankBigJoker] == 1 {
		out = append(out, rankGroup(Rocket, RankBigJoker, 1, RankSmallJoker, RankBigJoker))
	}
	return out
}

// seatTable 是替一个座位想一步时用到的局面。
type seatTable struct {
	k       Knowledge
	level   int
	rng     *rand.Rand
	hand    counts
	next    int
	partner int // 农民的队友，地主为 -1
	// unseen 是对手(和队友)手里可能有的牌：困难难度记牌，是自己没有、也没打出过的牌；其他难度只排除自己手里的牌。
	unseen counts
}

func newSeatTable(k Knowledge, level int, rng *rand.Rand) *seatTable {
	t := &seatTable{k: k, level: level, rng: rng, hand: countRanks(k.Hand), next: (k.Seat + 1) % Players, partner: -1}
	if k.Landlord >= 0 && k.Seat != k.Landlord {
		t.partner = Players - k.Seat - k.Landlord
	}
	for rank := 3; rank <= RankTwo; rank++ {
		t.unseen[rank] = 4
	}
	t.unseen[RankSmallJoker], t.unseen[RankBigJoker] = 1, 1
	for _, card := range k.Hand {
		t.unseen[Rank(card)]--
	}
	if level >= 3 {
		for _, card := range k.Played {
			t.unseen[Rank(card)]--
		}
	}
	return t
}

func (t *seatTable) isOpponent(seat int) bool {
	return seat != t.k.Seat && seat != t.partner
}

// fewestOpponentCards 是对手里最少的剩余张数。
func (t *seatTable) fewestOpponentCards() int {
	fewest := MaxCards
	for seat := 0; seat < Players; seat++ {
		if t.isOpponent(seat) {
			fewest = min(fewest, t.k.Counts[seat])
		}
	}
	return fewest
}

// unbeatable 表示对手手里不可能有压得过 g 的牌(按 unseen 估计，最多的对手也拿不出那么多张时也压不过)。
func (t *seatTable) unbeatable(g group) bool {
	most := 0
	for seat := 0; seat < Players; seat++ {
		if t.isOpponent(seat) {
			most = max(most, t.k.Counts[seat])
		}
	}
	for _, other := range beatingGroups(t.unseen, g.Combination) {
		if len(other.ranks) <= most {
			return false
		}
	}
	return true
}

// BestMove 为 k.Seat 选一步，level 为 1 到 3。rng 让 AI 在差不多的出法里不总是同一种。
func BestMove(k Knowledge, level int, rng *rand.Rand) (Move, error) {
	if level < 1 || level > 3 {
		return Move{}, ErrInvalidLevel
	}
	switch k.Phase {
	case PhaseBidding:
		bid := chooseBid(k, level, rng)
		return Move{Bid: &bid}, nil
	case PhasePlaying:
		return newSeatTable(k, level, rng).play(), nil
	}
	return Move{}, ErrIllegalMove
}

// HintMoves 只根据本座位已知的信息列出有限个合法选牌，顺序固定，不会提交出牌或修改手牌。
// 调用方负责确认轮到本座位；队友出牌也提供能压过的选牌，由玩家决定是否不出。
func HintMoves(k Knowledge) [][]int {
	hints := make([][]int, 0, MaxHints)
	if k.Phase != PhasePlaying || k.Seat < 0 || k.Seat >= Players || len(k.Hand) == 0 {
		return hints
	}
	t := newSeatTable(k, 2, nil)
	var groups []group
	var preferred []int
	if k.LastPlay != nil {
		groups = beatingGroups(t.hand, k.LastPlay.Combination)
	} else {
		preferred = t.cards(t.lead().ranks)
		for mode := 0; mode < planModes; mode++ {
			groups = append(groups, buildPlan(t.hand, mode)...)
		}
		// 拆好的整组之外，也给单张、对子、三张的备选；炸弹和王炸由同一候选器补齐。
		for _, kind := range []string{Single, Pair, Triple} {
			groups = append(groups, beatingGroups(t.hand, Combination{Type: kind, Rank: 2, Chain: 1})...)
		}
	}
	if combo, err := Classify(k.Hand); err == nil && (k.LastPlay == nil || combo.Beats(k.LastPlay.Combination)) {
		ranks := make([]int, len(k.Hand))
		for i, card := range k.Hand {
			ranks[i] = Rank(card)
		}
		groups = append(groups, group{Combination: combo, ranks: ranks})
	}
	type option struct {
		cards     []int
		bomb      bool
		preferred bool
		rank      int
		cost      int
	}
	options := make([]option, 0, len(groups))
	base := len(planHand(t.hand))
	for _, g := range groups {
		cards := t.cards(g.ranks)
		combo, err := Classify(cards)
		if err != nil || len(cards) != len(g.ranks) || k.LastPlay != nil && !combo.Beats(k.LastPlay.Combination) {
			continue
		}
		cost := (g.Rank-g.Chain+1)*10 - len(cards)
		if k.LastPlay != nil {
			rest := planHand(t.hand.without(g.ranks))
			penalty := (len(rest) - (base - 1)) * 100
			if g.isControl() {
				if base > 4 {
					penalty += 120
				} else {
					penalty += 40
				}
			}
			cost = penalty*10 + rest.cost() + g.Rank
		} else if g.isControl() {
			cost += 300
		}
		options = append(options, option{cards: cards, bomb: g.isBomb(), preferred: slices.Equal(cards, preferred), rank: g.Rank, cost: cost})
	}
	sort.SliceStable(options, func(i, j int) bool {
		a, b := options[i], options[j]
		// 能一次出完优先，其次领出时的普通 AI 推荐；其余先普通牌，炸弹和王炸留后。
		if (len(a.cards) == len(k.Hand)) != (len(b.cards) == len(k.Hand)) {
			return len(a.cards) == len(k.Hand)
		}
		if a.preferred != b.preferred {
			return a.preferred
		}
		if a.bomb != b.bomb {
			return !a.bomb
		}
		if a.bomb && a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		return slices.Compare(a.cards, b.cards) < 0
	})
	for _, candidate := range options {
		duplicate := false
		for _, existing := range hints {
			if slices.Equal(existing, candidate.cards) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			hints = append(hints, candidate.cards)
			if len(hints) == MaxHints {
				break
			}
		}
	}
	return hints
}

// bidStrength 估计一手牌当地主的把握：王、2 与炸弹是大牌，手数越少越好出完。
func bidStrength(c counts) int {
	strength := 0
	if c[RankSmallJoker] == 1 && c[RankBigJoker] == 1 {
		strength += 8
	} else {
		strength += 3*c[RankSmallJoker] + 4*c[RankBigJoker]
	}
	strength += 2 * c[RankTwo]
	for rank := 3; rank <= RankAce; rank++ {
		if c[rank] == 4 {
			strength += 5
		}
	}
	if c[RankAce] >= 2 {
		strength++
	}
	return strength + max(0, 11-len(planHand(c)))
}

func chooseBid(k Knowledge, level int, rng *rand.Rand) int {
	want := 0
	if level == 1 {
		if rng.IntN(3) == 0 {
			want = k.HighBid + 1
		}
	} else {
		switch strength := bidStrength(countRanks(k.Hand)); {
		case strength >= 11:
			want = 3
		case strength >= 9:
			want = 2
		case strength >= 7:
			want = 1
		}
	}
	if want <= k.HighBid {
		want = 0
	}
	if want == 0 && k.MustBid {
		want = k.HighBid + 1
	}
	return min(want, MaxBid)
}

// play 选一手牌或不出，结果一定是合法的一步：挑出来的牌对不上牌型或压不过上一手时(不应发生)，领出改出最小的单张，
// 跟牌改为不出。
func (t *seatTable) play() Move {
	var chosen *group
	if t.k.LastPlay == nil {
		g := t.lead()
		chosen = &g
	} else {
		chosen = t.follow()
	}
	if chosen != nil {
		cards := t.cards(chosen.ranks)
		combo, err := Classify(cards)
		if err == nil && (t.k.LastPlay == nil || combo.Beats(t.k.LastPlay.Combination)) {
			return Move{Cards: cards}
		}
	}
	if t.k.LastPlay != nil {
		return Move{Pass: true}
	}
	return Move{Cards: []int{slices.Min(t.k.Hand)}}
}

// cards 按点数从手牌里取出具体的牌。
func (t *seatTable) cards(ranks []int) []int {
	used := map[int]bool{}
	cards := make([]int, 0, len(ranks))
	for _, rank := range ranks {
		for _, card := range t.k.Hand {
			if Rank(card) == rank && !used[card] {
				used[card] = true
				cards = append(cards, card)
				break
			}
		}
	}
	sort.Ints(cards)
	return cards
}

// lowestGroup 是手里最小的单张(pair 为真时是最小的对子)，没有时 ok 为假。
func (t *seatTable) lowestGroup(pair bool) (group, bool) {
	for rank := 3; rank <= RankBigJoker; rank++ {
		switch {
		case pair && t.hand[rank] >= 2 && rank <= RankTwo:
			return rankGroup(Pair, rank, 1, rank, rank), true
		case !pair && t.hand[rank] >= 1:
			return rankGroup(Single, rank, 1, rank), true
		}
	}
	return group{}, false
}

// lead 领出一手。
func (t *seatTable) lead() group {
	p := planHand(t.hand)
	if len(p) == 1 {
		return p[0]
	}
	if t.level == 1 {
		normal := slices.DeleteFunc(slices.Clone(p), func(g group) bool { return g.isBomb() })
		if len(normal) == 0 {
			normal = p
		}
		return normal[t.rng.IntN(len(normal))]
	}
	// 队友是下家且只剩一两张：送最小的单张或对子让他走。
	if t.partner >= 0 && t.partner == t.next {
		switch t.k.Counts[t.partner] {
		case 1:
			if g, ok := t.lowestGroup(false); ok {
				return g
			}
		case 2:
			if g, ok := t.lowestGroup(true); ok {
				return g
			}
		}
	}
	// 残局：除了最后一手都没人压得住时，先出压不住的，最后一手出完。还有两手以上压得住时，压不住的大牌留着夺回出牌权。
	if t.canFinish(p) {
		for _, g := range p {
			if t.unbeatable(g) {
				return g
			}
		}
	}
	fewest := t.fewestOpponentCards()
	var best *group
	bestScore := 0
	for i := range p {
		g := p[i]
		// 起点小、张数多的先出(顺子按最小的一张算)；炸弹、2 与王留到最后。
		score := (g.Rank-g.Chain+1)*10 - len(g.ranks)
		switch {
		case g.isBomb():
			score += 1000
		case g.isControl():
			score += 300
		}
		// 对手只剩一张时不领单张，只剩两张时不领对子：实在要领，就领最大的、对手压不住的。
		if g.Type == Single && fewest == 1 || g.Type == Pair && fewest == 2 {
			if t.unbeatable(g) {
				score -= 100
			} else {
				score += 500 - g.Rank*20
			}
		}
		if best == nil || score < bestScore {
			best, bestScore = &p[i], score
		}
	}
	return *best
}

// follow 跟牌：返回要出的一手，nil 表示不出。
func (t *seatTable) follow() *group {
	previous := t.k.LastPlay
	options := beatingGroups(t.hand, previous.Combination)
	if len(options) == 0 {
		return nil
	}
	total := t.hand.total()
	for i := range options {
		if len(options[i].ranks) == total {
			return &options[i]
		}
	}
	if t.level == 1 {
		if previous.Seat == t.partner || t.rng.IntN(5) < 2 {
			return nil
		}
		normal := slices.DeleteFunc(slices.Clone(options), func(g group) bool { return g.isBomb() })
		if len(normal) == 0 {
			if t.k.Counts[previous.Seat] > 2 {
				return nil
			}
			normal = options
		}
		return &normal[t.rng.IntN(len(normal))]
	}
	if previous.Seat == t.partner {
		// 队友的牌不压。地主是下家且快出完时，用 A 以上的大牌顶住队友的小单张、小对子，免得地主接着出完。
		if t.next == t.k.Landlord && t.k.Counts[t.k.Landlord] <= 2 && (previous.Type == Single || previous.Type == Pair) && previous.Rank < RankAce {
			for i := len(options) - 1; i >= 0; i-- {
				if !options[i].isBomb() && options[i].Rank >= RankAce {
					return &options[i]
				}
			}
		}
		return nil
	}
	base := len(planHand(t.hand))
	cards := t.k.Counts[previous.Seat]
	// 能接受的代价：出牌的对手快出完时什么都肯拆；农民要顶住地主的牌，不让地主一直用小牌走，比地主更肯压。
	limit := 100
	switch {
	case cards <= 2 || t.fewestOpponentCards() <= 2:
		limit = 1 << 30
	case cards <= 5 && t.partner >= 0:
		limit = 300
	case cards <= 5 || t.partner >= 0:
		limit = 200
	}
	var best *group
	bestCost := 0
	for i := range options {
		g := options[i]
		if g.isBomb() {
			continue
		}
		rest := planHand(t.hand.without(g.ranks))
		// 正好出了拆好的一组时 rest 比 base 少一手，多出来的手数就是拆坏的代价。
		penalty := (len(rest) - (base - 1)) * 100
		if g.isControl() {
			if base > 4 {
				penalty += 120
			} else {
				penalty += 40
			}
		}
		if penalty > limit {
			continue
		}
		cost := penalty*10 + rest.cost() + g.Rank
		if best == nil || cost < bestCost {
			best, bestCost = &options[i], cost
		}
	}
	if best != nil {
		return best
	}
	// 只有炸弹压得住：对手快出完，或者炸了之后自己两手之内出完(困难难度还看剩下的能不能都出得去)才炸，先用小的。
	for i := range options {
		g := options[i]
		if !g.isBomb() {
			continue
		}
		rest := planHand(t.hand.without(g.ranks))
		if cards <= 4 || len(rest) <= 2 || t.level >= 3 && t.canFinish(rest) {
			return &options[i]
		}
	}
	return nil
}

// canFinish 表示拿到出牌权后能把 rest 出完：除了最后一手，每一手都没人压得住。
func (t *seatTable) canFinish(rest plan) bool {
	beatable := 0
	for _, g := range rest {
		if !t.unbeatable(g) {
			beatable++
		}
	}
	return beatable <= 1
}
