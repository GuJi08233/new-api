// Package landlord 是游戏中心斗地主(三人对战)的规则：发牌、叫分、牌型、出牌与胜负。
//
// 一副 54 张牌，三人各 17 张，留 3 张底牌。首叫座位随机，依次选择不叫或 1～3 分，叫分必须高于已有的叫分；叫 3 分直接
// 当地主，否则三人都叫过后最高分的当地主。三家都不叫就重新洗牌、由下一位首叫；第 MaxDeals 次发牌时前两位都没叫，最后一位
// 不能再不叫，对局一定能开始。地主拿走底牌并先出牌。
//
// 牌型：单张、对子、三张、三带一、三带二、顺子(5 张以上)、连对(3 对以上)、飞机(2 组以上连续的三张)、飞机带单牌或对子、
// 四带二(两张单牌或一对)、四带两对、炸弹与王炸。顺子、连对与飞机机身不能含 2 和王；飞机的翅膀不能与机身同点数，飞机带
// 单牌时翅膀可以是对子；四带两对的两对点数不同。同类型、同长度的牌按最高点数比大小，炸弹压普通牌型，王炸最大。
//
// 轮到的玩家出一手压过上一手的牌或不出；连续两人不出时，最后出牌的玩家重新领出，领出时不能不出。地主先出完则地主获胜，
// 赢两倍底注、两位农民各输一倍；任一农民先出完则两位农民共同获胜，地主输两倍、农民各赢一倍。叫分只决定谁当地主，叫分与
// 炸弹都不加倍。
package landlord

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
)

const (
	Players    = 3
	DeckSize   = 54
	HandSize   = 17
	BottomSize = 3
	MaxBid     = 3
	// MaxDeals 是一局最多发几次牌，见包说明。
	MaxDeals = 3
	// MaxCards 是一手牌最多的张数：地主拿到底牌后有 20 张。
	MaxCards = HandSize + BottomSize
)

// 牌号 0..51 按点数 3、4、…、K、A、2 排列，每种点数四张(牌号除以 4 的余数是花色)；52 是小王，53 是大王。
// 点数 3..14 是 3 到 A，15 是 2，16 是小王，17 是大王。
const (
	SmallJoker     = 52
	BigJoker       = 53
	RankAce        = 14
	RankTwo        = 15
	RankSmallJoker = 16
	RankBigJoker   = 17
)

// 牌型。
const (
	Single         = "single"
	Pair           = "pair"
	Triple         = "triple"
	TripleSingle   = "triple_single"
	TriplePair     = "triple_pair"
	Straight       = "straight"
	PairStraight   = "pair_straight"
	Airplane       = "airplane"
	AirplaneSingle = "airplane_single"
	AirplanePair   = "airplane_pair"
	FourTwo        = "four_two"
	FourTwoPairs   = "four_two_pairs"
	Bomb           = "bomb"
	Rocket         = "rocket"
)

// 对局阶段。
const (
	PhaseBidding  = "bidding"
	PhasePlaying  = "playing"
	PhaseFinished = "finished"
)

var ErrIllegalMove = errors.New("landlord: illegal move")

// Rank 返回一张牌的点数。
func Rank(card int) int {
	if card >= SmallJoker {
		return card - SmallJoker + RankSmallJoker
	}
	return card/4 + 3
}

// Combination 是一手牌的牌型：Rank 是比大小用的点数(带牌的牌型看三张或四张的部分，序列看最高的一组)，Chain 是序列的组数，
// 其他牌型为 1。
type Combination struct {
	Type  string `json:"type"`
	Rank  int    `json:"rank"`
	Chain int    `json:"chain"`
}

// Classify 识别一手牌的牌型，不是合法牌型(含重复或不存在的牌)时返回 ErrIllegalMove。
func Classify(cards []int) (Combination, error) {
	bad := Combination{}
	if len(cards) == 0 || len(cards) > MaxCards {
		return bad, ErrIllegalMove
	}
	var seen [DeckSize]bool
	var counts [RankBigJoker + 1]int
	for _, card := range cards {
		if card < 0 || card >= DeckSize || seen[card] {
			return bad, ErrIllegalMove
		}
		seen[card] = true
		counts[Rank(card)]++
	}
	if len(cards) == 2 && counts[RankSmallJoker] == 1 && counts[RankBigJoker] == 1 {
		return Combination{Type: Rocket, Rank: RankBigJoker, Chain: 1}, nil
	}
	var ranks []int
	var groups [5][]int
	for rank := 3; rank < len(counts); rank++ {
		if counts[rank] > 0 {
			ranks = append(ranks, rank)
			groups[counts[rank]] = append(groups[counts[rank]], rank)
		}
	}
	if len(ranks) == 1 {
		kind := [...]string{"", Single, Pair, Triple, Bomb}
		return Combination{Type: kind[len(cards)], Rank: ranks[0], Chain: 1}, nil
	}
	if len(groups[3]) == 1 {
		if len(cards) == 4 && len(groups[1]) == 1 {
			return Combination{Type: TripleSingle, Rank: groups[3][0], Chain: 1}, nil
		}
		if len(cards) == 5 && len(groups[2]) == 1 {
			return Combination{Type: TriplePair, Rank: groups[3][0], Chain: 1}, nil
		}
	}
	if len(groups[4]) == 1 {
		if len(cards) == 6 && (len(groups[1]) == 2 || len(groups[2]) == 1) {
			return Combination{Type: FourTwo, Rank: groups[4][0], Chain: 1}, nil
		}
		if len(cards) == 8 && len(groups[2]) == 2 {
			return Combination{Type: FourTwoPairs, Rank: groups[4][0], Chain: 1}, nil
		}
	}
	// 每个点数张数相同、首尾相差正好是点数个数减一，就是连续的序列。
	if ranks[len(ranks)-1] <= RankAce && ranks[len(ranks)-1]-ranks[0]+1 == len(ranks) {
		if len(cards) >= 5 && len(groups[1]) == len(ranks) {
			return Combination{Type: Straight, Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
		if len(ranks) >= 3 && len(groups[2]) == len(ranks) {
			return Combination{Type: PairStraight, Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
		if len(ranks) >= 2 && len(groups[3]) == len(ranks) {
			return Combination{Type: Airplane, Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
	}
	for _, wing := range []struct {
		size int
		kind string
	}{{4, AirplaneSingle}, {5, AirplanePair}} {
		if len(cards)%wing.size != 0 {
			continue
		}
		chain := len(cards) / wing.size
		if chain < 2 {
			continue
		}
		for first := 3; first+chain-1 <= RankAce; first++ {
			valid, pairs := true, 0
			for rank := 3; rank < len(counts); rank++ {
				if rank >= first && rank < first+chain {
					if counts[rank] != 3 {
						valid = false
						break
					}
					continue
				}
				if counts[rank] > 2 || wing.size == 5 && counts[rank] == 1 {
					valid = false
					break
				}
				if counts[rank] == 2 {
					pairs++
				}
			}
			// 带单牌时翅膀张数由总张数保证；带对子时每个翅膀点数都是一对，对数必须等于组数。
			if valid && (wing.size == 4 || pairs == chain) {
				return Combination{Type: wing.kind, Rank: first + chain - 1, Chain: chain}, nil
			}
		}
	}
	return bad, ErrIllegalMove
}

// Beats 表示 c 能压过 previous。
func (c Combination) Beats(previous Combination) bool {
	if previous.Type == Rocket {
		return false
	}
	if c.Type == Rocket {
		return true
	}
	if c.Type == Bomb && previous.Type != Bomb {
		return true
	}
	return c.Type == previous.Type && c.Chain == previous.Chain && c.Rank > previous.Rank
}

// Play 是一手出牌，Cards 从小到大排列。
type Play struct {
	Combination
	Seat  int   `json:"seat"`
	Cards []int `json:"cards"`
}

// Action 是一轮里的一次出牌(Cards 不为空)或不出(Pass)。
type Action struct {
	Seat  int    `json:"seat"`
	Pass  bool   `json:"pass,omitempty"`
	Type  string `json:"type,omitempty"`
	Cards []int  `json:"cards,omitempty"`
}

// Game 是一局斗地主，整份以 JSON 存在房间或人机对局里。
type Game struct {
	Phase  string         `json:"phase"`
	Hands  [Players][]int `json:"hands"`
	Bottom []int          `json:"bottom"`
	// Bids 是这一次发牌里各座位的叫分，-1 是还没轮到，0 是不叫。
	Bids       [Players]int `json:"bids"`
	BidFirst   int          `json:"bid_first"`
	BidCount   int          `json:"bid_count"`
	HighBid    int          `json:"high_bid"`
	HighBidder int          `json:"high_bidder"`
	// Deals 是第几次发牌。
	Deals    int `json:"deals"`
	Landlord int `json:"landlord"`
	ToMove   int `json:"to_move"`
	// LastPlay 是这一轮要压过的一手，nil 表示轮到的玩家领出。
	LastPlay *Play `json:"last_play"`
	// Trick 是这一轮的出牌与不出，以领出的一手开头。一轮在连续两人不出时结束，结束的一轮留到下一手领出时才清掉，
	// 好让玩家看到别人怎么出的。
	Trick []Action `json:"trick"`
	// Played 是已经打出的牌，按出牌先后排列。
	Played []int `json:"played"`
	// Winner 是先出完牌的座位，没结束时为 -1。
	Winner int `json:"winner"`
}

// Shuffle 打乱一副牌，默认用密码学安全的随机数，玩家猜不到下一副牌。测试里换成固定的顺序。
var Shuffle = func(deck []int) error {
	for i := len(deck) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		deck[i], deck[j.Int64()] = deck[j.Int64()], deck[i]
	}
	return nil
}

// New 洗牌发牌开始一局，first 是首叫的座位。
func New(first int) (*Game, error) {
	if first < 0 || first >= Players {
		return nil, fmt.Errorf("landlord: invalid first seat %d", first)
	}
	game := &Game{}
	return game, game.deal(first, 1)
}

// deal 洗一副新牌发给三家，这是第 deals 次发牌。
func (g *Game) deal(first int, deals int) error {
	deck := make([]int, DeckSize)
	for i := range deck {
		deck[i] = i
	}
	if err := Shuffle(deck); err != nil {
		return err
	}
	*g = Game{
		Phase: PhaseBidding, Bottom: slices.Clone(deck[Players*HandSize:]), Bids: [Players]int{-1, -1, -1},
		BidFirst: first, HighBidder: -1, Deals: deals, Landlord: -1, ToMove: first,
		Trick: []Action{}, Played: []int{}, Winner: -1,
	}
	for seat := range g.Hands {
		g.Hands[seat] = slices.Clone(deck[seat*HandSize : (seat+1)*HandSize])
		sort.Ints(g.Hands[seat])
	}
	return nil
}

// Over 表示已经有人出完了牌。
func (g *Game) Over() bool {
	return g.Phase == PhaseFinished
}

// MustBid 表示轮到的玩家不能不叫：第 MaxDeals 次发牌时前两位都没叫，最后一位必须叫分。
func (g *Game) MustBid() bool {
	return g.Phase == PhaseBidding && g.Deals >= MaxDeals && g.BidCount == Players-1 && g.HighBid == 0
}

// Bid 让 seat 叫 bid 分，0 是不叫。三家都叫过或有人叫了 3 分时定下地主；三家都不叫就重新发牌。
func (g *Game) Bid(seat int, bid int) error {
	if g.Phase != PhaseBidding || seat != g.ToMove || bid < 0 || bid > MaxBid ||
		bid != 0 && bid <= g.HighBid || bid == 0 && g.MustBid() {
		return ErrIllegalMove
	}
	g.Bids[seat] = bid
	g.BidCount++
	if bid > g.HighBid {
		g.HighBid, g.HighBidder = bid, seat
	}
	if bid < MaxBid && g.BidCount < Players {
		g.ToMove = (seat + 1) % Players
		return nil
	}
	if g.HighBid == 0 {
		return g.deal((g.BidFirst+1)%Players, g.Deals+1)
	}
	g.Phase, g.Landlord, g.ToMove = PhasePlaying, g.HighBidder, g.HighBidder
	g.Hands[g.Landlord] = append(g.Hands[g.Landlord], g.Bottom...)
	sort.Ints(g.Hands[g.Landlord])
	return nil
}

// Play 让 seat 出 cards，必须是手里的牌、合法的牌型，并且领出或压过这一轮的上一手。出完手牌时对局结束。
func (g *Game) Play(seat int, cards []int) error {
	if g.Phase != PhasePlaying || seat != g.ToMove {
		return ErrIllegalMove
	}
	combo, err := Classify(cards)
	if err != nil || g.LastPlay != nil && !combo.Beats(g.LastPlay.Combination) {
		return ErrIllegalMove
	}
	var inHand, chosen [DeckSize]bool
	for _, card := range g.Hands[seat] {
		inHand[card] = true
	}
	for _, card := range cards {
		if !inHand[card] {
			return ErrIllegalMove
		}
		chosen[card] = true
	}
	hand := make([]int, 0, len(g.Hands[seat])-len(cards))
	for _, card := range g.Hands[seat] {
		if !chosen[card] {
			hand = append(hand, card)
		}
	}
	played := slices.Clone(cards)
	sort.Ints(played)
	if g.LastPlay == nil {
		g.Trick = []Action{}
	}
	g.Hands[seat] = hand
	g.Played = append(g.Played, played...)
	g.Trick = append(g.Trick, Action{Seat: seat, Type: combo.Type, Cards: played})
	g.LastPlay = &Play{Combination: combo, Seat: seat, Cards: played}
	g.ToMove = (seat + 1) % Players
	if len(hand) == 0 {
		g.Phase, g.ToMove, g.Winner = PhaseFinished, -1, seat
	}
	return nil
}

// Pass 让 seat 不出。领出时不能不出；两人都不出之后，最后出牌的玩家重新领出。
func (g *Game) Pass(seat int) error {
	if g.Phase != PhasePlaying || seat != g.ToMove || g.LastPlay == nil {
		return ErrIllegalMove
	}
	g.Trick = append(g.Trick, Action{Seat: seat, Pass: true})
	g.ToMove = (seat + 1) % Players
	if g.ToMove == g.LastPlay.Seat {
		g.LastPlay = nil
	}
	return nil
}

// Scores 返回结束的一局里每个座位输赢几倍底注，合计为 0：地主先出完赢两倍、两位农民各输一倍，农民先出完则反过来。
// 还没结束时返回 nil。
func (g *Game) Scores() []int {
	if g.Phase != PhaseFinished {
		return nil
	}
	sign := -1
	if g.Winner == g.Landlord {
		sign = 1
	}
	scores := make([]int, Players)
	for seat := range scores {
		scores[seat] = -sign
	}
	scores[g.Landlord] = 2 * sign
	return scores
}

// Validate 检查保存下来的一局前后一致：叫分顺序、阶段、每张牌恰好出现一次、这一轮的出牌顺序与压牌关系。损坏的状态不能
// 继续出牌，也不能拿去结算。
func (g *Game) Validate() error {
	if g.Phase != PhaseBidding && g.Phase != PhasePlaying && g.Phase != PhaseFinished {
		return errors.New("landlord: invalid phase")
	}
	if g.Deals < 1 || g.Deals > MaxDeals || g.BidFirst < 0 || g.BidFirst >= Players || g.BidCount < 0 || g.BidCount > Players ||
		len(g.Bottom) != BottomSize {
		return errors.New("landlord: invalid deal")
	}
	// 从首叫座位起最多三次叫分，3 分只能是最后一次；还没轮到的座位为 -1。
	highBid, highBidder := 0, -1
	for i := 0; i < Players; i++ {
		seat := (g.BidFirst + i) % Players
		bid := g.Bids[seat]
		if i >= g.BidCount {
			if bid != -1 {
				return errors.New("landlord: unexpected bid")
			}
			continue
		}
		if highBid == MaxBid || bid < 0 || bid > MaxBid || bid != 0 && bid <= highBid {
			return errors.New("landlord: invalid bid sequence")
		}
		if bid > highBid {
			highBid, highBidder = bid, seat
		}
	}
	if g.HighBid != highBid || g.HighBidder != highBidder {
		return errors.New("landlord: invalid highest bid")
	}
	if g.Phase == PhaseBidding {
		if g.BidCount >= Players || g.HighBid == MaxBid || g.Landlord != -1 || g.Winner != -1 || g.LastPlay != nil ||
			len(g.Trick) != 0 || len(g.Played) != 0 || g.ToMove != (g.BidFirst+g.BidCount)%Players {
			return errors.New("landlord: invalid bidding state")
		}
	} else if g.Landlord < 0 || g.Landlord >= Players || g.HighBid == 0 || g.HighBidder != g.Landlord ||
		g.HighBid != MaxBid && g.BidCount != Players {
		return errors.New("landlord: invalid landlord")
	}
	if g.Phase == PhaseFinished {
		if g.ToMove != -1 || g.Winner < 0 || g.Winner >= Players || len(g.Hands[g.Winner]) != 0 {
			return errors.New("landlord: invalid winner")
		}
	} else if g.ToMove < 0 || g.ToMove >= Players || g.Winner != -1 {
		return errors.New("landlord: invalid turn")
	}
	var seen [DeckSize]bool
	mark := func(cards []int) error {
		for _, card := range cards {
			if card < 0 || card >= DeckSize || seen[card] {
				return errors.New("landlord: invalid card")
			}
			seen[card] = true
		}
		return nil
	}
	for seat, hand := range g.Hands {
		if len(hand) > MaxCards || g.Phase == PhaseBidding && len(hand) != HandSize || g.Phase == PhasePlaying && len(hand) == 0 {
			return fmt.Errorf("landlord: invalid hand at seat %d", seat)
		}
		if err := mark(hand); err != nil {
			return err
		}
	}
	rest := g.Played
	if g.Phase == PhaseBidding {
		rest = g.Bottom
	}
	if err := mark(rest); err != nil {
		return err
	}
	for _, exists := range seen {
		if !exists {
			return errors.New("landlord: incomplete deck")
		}
	}
	var bottom [DeckSize]bool
	for _, card := range g.Bottom {
		if card < 0 || card >= DeckSize || bottom[card] {
			return errors.New("landlord: invalid bottom")
		}
		bottom[card] = true
	}
	if g.Phase == PhaseBidding {
		return nil
	}
	// 底牌发给了地主：要么还在地主手里，要么已经打出。
	for _, card := range g.Bottom {
		if !slices.Contains(g.Hands[g.Landlord], card) && !slices.Contains(g.Played, card) {
			return errors.New("landlord: bottom cards left the landlord")
		}
	}
	return g.validateTrick()
}

// validateTrick 检查这一轮：领出的一手开头，按座位顺序轮流，每手都压过前一手，最多连续两人不出且只能在末尾；
// 最近打出的牌正好是这一轮出的牌，上一手与轮到谁都和这一轮一致。
func (g *Game) validateTrick() error {
	if len(g.Trick) == 0 {
		if len(g.Played) != 0 || g.LastPlay != nil || g.ToMove != g.Landlord {
			return errors.New("landlord: invalid opening")
		}
		return nil
	}
	var last *Play
	var cards []int
	passes := 0
	for i, action := range g.Trick {
		if action.Seat < 0 || action.Seat >= Players || i > 0 && action.Seat != (g.Trick[i-1].Seat+1)%Players {
			return errors.New("landlord: invalid trick order")
		}
		if action.Pass {
			if last == nil || passes == Players-1 || len(action.Cards) != 0 || action.Type != "" {
				return errors.New("landlord: invalid pass")
			}
			passes++
			continue
		}
		if passes == Players-1 {
			return errors.New("landlord: trick continued after it ended")
		}
		combo, err := Classify(action.Cards)
		if err != nil || combo.Type != action.Type || !slices.IsSorted(action.Cards) || last != nil && !combo.Beats(last.Combination) {
			return errors.New("landlord: invalid trick play")
		}
		last = &Play{Combination: combo, Seat: action.Seat, Cards: action.Cards}
		cards = append(cards, action.Cards...)
		passes = 0
	}
	if len(cards) > len(g.Played) || !slices.Equal(g.Played[len(g.Played)-len(cards):], cards) {
		return errors.New("landlord: trick does not match played cards")
	}
	end := g.Trick[len(g.Trick)-1]
	switch {
	case g.Phase == PhaseFinished:
		if end.Pass || end.Seat != g.Winner || g.LastPlay == nil {
			return errors.New("landlord: invalid final play")
		}
	case g.ToMove != (end.Seat+1)%Players:
		return errors.New("landlord: invalid trick turn")
	}
	if passes == Players-1 {
		if g.LastPlay != nil {
			return errors.New("landlord: ended trick still has a play to beat")
		}
		return nil
	}
	if g.LastPlay == nil || g.LastPlay.Seat != last.Seat || g.LastPlay.Combination != last.Combination || !slices.Equal(g.LastPlay.Cards, last.Cards) {
		return errors.New("landlord: invalid last play")
	}
	return nil
}

// View 是给 seat 看的局面：自己的手牌、各家剩余张数与桌上公开的牌；seat 不是座位时(旁观)看不到任何手牌。叫分时底牌不公开，
// 正常结束后公开各家剩下的牌。Played 是这一局打出过的全部牌(人人都看见过)，页面据此做记牌器。
type View struct {
	Phase    string          `json:"phase"`
	Hand     []int           `json:"hand"`
	Counts   [Players]int    `json:"counts"`
	Bottom   []int           `json:"bottom"`
	Bids     [Players]int    `json:"bids"`
	HighBid  int             `json:"high_bid"`
	MustBid  bool            `json:"must_bid"`
	Deals    int             `json:"deals"`
	Landlord int             `json:"landlord"`
	ToMove   int             `json:"to_move"`
	LastPlay *Play           `json:"last_play"`
	Trick    []Action        `json:"trick"`
	Played   []int           `json:"played"`
	Over     bool            `json:"over"`
	Winner   int             `json:"winner"`
	Hands    *[Players][]int `json:"hands,omitempty"`
}

func (g *Game) View(seat int) View {
	view := View{
		Phase: g.Phase, Hand: []int{}, Bottom: []int{}, Bids: g.Bids, HighBid: g.HighBid, MustBid: g.MustBid(),
		Deals: g.Deals, Landlord: g.Landlord, ToMove: g.ToMove, LastPlay: g.LastPlay, Trick: g.Trick,
		Played: append([]int{}, g.Played...), Over: g.Over(), Winner: g.Winner,
	}
	for member, hand := range g.Hands {
		view.Counts[member] = len(hand)
	}
	if seat >= 0 && seat < Players {
		view.Hand = append(view.Hand, g.Hands[seat]...)
	}
	if g.Phase != PhaseBidding {
		view.Bottom = append(view.Bottom, g.Bottom...)
	}
	if view.Over {
		hands := g.Hands
		view.Hands = &hands
	}
	return view
}

// Knowledge 是 seat 能知道的全部信息：自己的手牌与桌上公开的一切，不含别人的手牌。AI 只看它，不会偷看。
type Knowledge struct {
	Seat     int
	Phase    string
	Hand     []int
	Counts   [Players]int
	Bids     [Players]int
	HighBid  int
	MustBid  bool
	Landlord int
	// Bottom 在叫分阶段为空。
	Bottom   []int
	LastPlay *Play
	Trick    []Action
	Played   []int
}

func (g *Game) Knowledge(seat int) Knowledge {
	view := g.View(seat)
	return Knowledge{
		Seat: seat, Phase: g.Phase, Hand: view.Hand, Counts: view.Counts, Bids: g.Bids, HighBid: g.HighBid,
		MustBid: view.MustBid, Landlord: g.Landlord, Bottom: view.Bottom, LastPlay: g.LastPlay,
		Trick: slices.Clone(g.Trick), Played: slices.Clone(g.Played),
	}
}
