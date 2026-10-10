package controller

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/QuantumNous/new-api/common"
)

const gameLandlord = "landlord"

func init() {
	registerPvpGame(gameLandlord, "斗地主", landlordEngine{})
}

// 牌号 0..51 按 3、4、...、A、2 排列，每种点数四张；52 是小王，53 是大王。
// 三人各押相同的固定金额，叫分和炸弹只影响牌局，不改变奖池或追加扣币。
type landlordEngine struct{}

type landlordMove struct {
	Bid   *int  `json:"bid"`
	Cards []int `json:"cards"`
	Pass  *bool `json:"pass"`
}

type landlordCombination struct {
	Type  string `json:"type"`
	Rank  int    `json:"rank"`
	Chain int    `json:"chain"`
}

type landlordPlay struct {
	landlordCombination
	Seat  int   `json:"seat"`
	Cards []int `json:"cards"`
}

type landlordState struct {
	Phase      string        `json:"phase"`
	Hands      [3][]int      `json:"hands"`
	Bottom     []int         `json:"bottom"`
	Bids       [3]int        `json:"bids"`
	BidFirst   int           `json:"bid_first"`
	BidCount   int           `json:"bid_count"`
	HighBid    int           `json:"high_bid"`
	HighBidder int           `json:"high_bidder"`
	Deals      int           `json:"deals"`
	Landlord   int           `json:"landlord"`
	ToMove     int           `json:"to_move"`
	LastPlay   *landlordPlay `json:"last_play"`
	Passes     int           `json:"passes"`
	Passed     [3]bool       `json:"passed"`
	Played     []int         `json:"played"`
	Winner     int           `json:"winner"`
}

type landlordView struct {
	Phase    string        `json:"phase"`
	Hand     []int         `json:"hand"`
	Counts   [3]int        `json:"counts"`
	Bottom   []int         `json:"bottom"`
	Bids     [3]int        `json:"bids"`
	HighBid  int           `json:"high_bid"`
	Deals    int           `json:"deals"`
	Landlord int           `json:"landlord"`
	ToMove   int           `json:"to_move"`
	LastPlay *landlordPlay `json:"last_play"`
	Passed   [3]bool       `json:"passed"`
	Over     bool          `json:"over"`
	Winner   int           `json:"winner"`
	Winners  []int         `json:"winners"`
	Hands    *[3][]int     `json:"hands,omitempty"`
}

func (landlordEngine) Seats() int { return 3 }

func landlordCardRank(card int) int {
	if card >= 52 {
		return card - 36
	}
	return card/4 + 3
}

// landlordClassify 校验整组牌并识别牌型。序列不能包含 2 或王；飞机机身与翅膀不能共用点数。
// 飞机单翼可含对子，四带二可带一个对子；飞机双翼和四带两对必须是不同点数的对子。
func landlordClassify(cards []int) (landlordCombination, error) {
	bad := landlordCombination{}
	if len(cards) == 0 || len(cards) > 20 {
		return bad, errPvpIllegalMove
	}
	var seen [54]bool
	var counts [18]int
	for _, card := range cards {
		if card < 0 || card >= len(seen) || seen[card] {
			return bad, errPvpIllegalMove
		}
		seen[card] = true
		counts[landlordCardRank(card)]++
	}
	if len(cards) == 2 && counts[16] == 1 && counts[17] == 1 {
		return landlordCombination{Type: "rocket", Rank: 17, Chain: 1}, nil
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
		kind := [...]string{"", "single", "pair", "triple", "bomb"}
		return landlordCombination{Type: kind[len(cards)], Rank: ranks[0], Chain: 1}, nil
	}
	if len(groups[3]) == 1 {
		if len(cards) == 4 && len(groups[1]) == 1 {
			return landlordCombination{Type: "triple_single", Rank: groups[3][0], Chain: 1}, nil
		}
		if len(cards) == 5 && len(groups[2]) == 1 {
			return landlordCombination{Type: "triple_pair", Rank: groups[3][0], Chain: 1}, nil
		}
	}
	if len(groups[4]) == 1 {
		if len(cards) == 6 && (len(groups[1]) == 2 || len(groups[2]) == 1) {
			return landlordCombination{Type: "four_two", Rank: groups[4][0], Chain: 1}, nil
		}
		if len(cards) == 8 && len(groups[2]) == 2 {
			return landlordCombination{Type: "four_two_pairs", Rank: groups[4][0], Chain: 1}, nil
		}
	}
	// 同点数组数一致且首尾长度与点数数量相等，就是连续序列。
	if ranks[len(ranks)-1] <= 14 && ranks[len(ranks)-1]-ranks[0]+1 == len(ranks) {
		if len(cards) >= 5 && len(groups[1]) == len(ranks) {
			return landlordCombination{Type: "straight", Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
		if len(ranks) >= 3 && len(groups[2]) == len(ranks) {
			return landlordCombination{Type: "pair_straight", Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
		if len(ranks) >= 2 && len(groups[3]) == len(ranks) {
			return landlordCombination{Type: "airplane", Rank: ranks[len(ranks)-1], Chain: len(ranks)}, nil
		}
	}
	for _, wing := range []struct {
		size int
		kind string
	}{{4, "airplane_single"}, {5, "airplane_pair"}} {
		if len(cards)%wing.size != 0 {
			continue
		}
		chain := len(cards) / wing.size
		if chain < 2 {
			continue
		}
		for first := 3; first+chain-1 <= 14; first++ {
			valid, singles, pairs := true, 0, 0
			for rank := 3; rank < len(counts); rank++ {
				if rank >= first && rank < first+chain {
					if counts[rank] != 3 {
						valid = false
						break
					}
					continue
				}
				if counts[rank] > 2 || wing.size == 5 && counts[rank] != 0 && counts[rank] != 2 {
					valid = false
					break
				}
				singles += counts[rank]
				if counts[rank] == 2 {
					pairs++
				}
			}
			if valid && (wing.size == 4 && singles == chain || wing.size == 5 && pairs == chain) {
				return landlordCombination{Type: wing.kind, Rank: first + chain - 1, Chain: chain}, nil
			}
		}
	}
	return bad, errPvpIllegalMove
}

func (play landlordCombination) beats(previous landlordCombination) bool {
	if previous.Type == "rocket" {
		return false
	}
	if play.Type == "rocket" {
		return true
	}
	if play.Type == "bomb" && previous.Type != "bomb" {
		return true
	}
	return play.Type == previous.Type && play.Chain == previous.Chain && play.Rank > previous.Rank
}

// deal 使用密码学随机数洗牌；无人叫地主时轮换首叫座位并重新洗全部 54 张牌。
func (state *landlordState) deal(first int) error {
	deck := make([]int, 54)
	for i := range deck {
		deck[i] = i
	}
	for i := len(deck) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		deck[i], deck[j.Int64()] = deck[j.Int64()], deck[i]
	}
	deals := state.Deals + 1
	*state = landlordState{
		Phase: "bidding", Bottom: append([]int{}, deck[51:]...), Bids: [3]int{-1, -1, -1},
		BidFirst: first, HighBidder: -1, Deals: deals, Landlord: -1, ToMove: first,
		Played: []int{}, Winner: -1,
	}
	for seat := range state.Hands {
		state.Hands[seat] = append([]int{}, deck[seat*17:(seat+1)*17]...)
		sort.Ints(state.Hands[seat])
	}
	return nil
}

// loadLandlord 检查持久状态的牌守恒与阶段，避免损坏的牌局被继续出牌或用于结算。
func loadLandlord(data []byte) (*landlordState, error) {
	var state landlordState
	if err := common.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Deals < 1 || state.BidFirst < 0 || state.BidFirst > 2 || state.BidCount < 0 || state.BidCount > 3 ||
		state.HighBid < 0 || state.HighBid > 3 || state.HighBidder < -1 || state.HighBidder > 2 ||
		state.Passes < 0 || state.Passes > 1 || len(state.Bottom) != 3 {
		return nil, errors.New("invalid landlord state")
	}
	if state.Phase != "bidding" && state.Phase != "playing" && state.Phase != "finished" {
		return nil, errors.New("invalid landlord phase")
	}
	// 按首叫座位检查最多三次叫分，3 分必须是最后一次。未轮到的座位仍为 -1。
	highBid, highBidder := 0, -1
	for i := 0; i < 3; i++ {
		seat := (state.BidFirst + i) % 3
		bid := state.Bids[seat]
		if i >= state.BidCount {
			if bid != -1 {
				return nil, errors.New("unexpected landlord bid")
			}
			continue
		}
		if highBid == 3 || bid < 0 || bid > 3 || bid != 0 && bid <= highBid {
			return nil, errors.New("invalid landlord bid sequence")
		}
		if bid > highBid {
			highBid, highBidder = bid, seat
		}
	}
	if state.HighBid != highBid || state.HighBidder != highBidder {
		return nil, errors.New("invalid landlord highest bid")
	}
	if state.Phase == "bidding" {
		if state.BidCount > 2 || state.HighBid == 3 || state.Landlord != -1 || state.Winner != -1 ||
			state.LastPlay != nil || len(state.Played) != 0 ||
			state.ToMove != (state.BidFirst+state.BidCount)%3 {
			return nil, errors.New("invalid landlord bidding state")
		}
	} else if state.Landlord < 0 || state.Landlord > 2 || state.HighBid == 0 || state.HighBidder != state.Landlord ||
		state.BidCount < 1 || state.HighBid != 3 && state.BidCount != 3 {
		return nil, errors.New("invalid landlord seat")
	}
	if state.Phase == "finished" {
		if state.ToMove != -1 || state.Winner < 0 || state.Winner > 2 || len(state.Hands[state.Winner]) != 0 ||
			state.LastPlay == nil || state.LastPlay.Seat != state.Winner || state.Passes != 0 {
			return nil, errors.New("invalid landlord winner")
		}
	} else if state.ToMove < 0 || state.ToMove > 2 || state.Winner != -1 {
		return nil, errors.New("invalid landlord turn")
	}
	var seen [54]bool
	for seat, hand := range state.Hands {
		if len(hand) > 20 || state.Phase == "bidding" && len(hand) != 17 || state.Phase == "playing" && len(hand) == 0 {
			return nil, fmt.Errorf("invalid landlord hand at seat %d", seat)
		}
		for _, card := range hand {
			if card < 0 || card >= 54 || seen[card] {
				return nil, errors.New("invalid landlord hand card")
			}
			seen[card] = true
		}
	}
	remaining := state.Played
	if state.Phase == "bidding" {
		remaining = state.Bottom
	}
	for _, card := range remaining {
		if card < 0 || card >= 54 || seen[card] {
			return nil, errors.New("invalid landlord played card")
		}
		seen[card] = true
	}
	for _, exists := range seen {
		if !exists {
			return nil, errors.New("incomplete landlord deck")
		}
	}
	var bottom [54]bool
	for _, card := range state.Bottom {
		if card < 0 || card >= 54 || bottom[card] {
			return nil, errors.New("invalid landlord bottom card")
		}
		bottom[card] = true
	}
	if state.LastPlay != nil {
		combo, err := landlordClassify(state.LastPlay.Cards)
		if err != nil || combo != state.LastPlay.landlordCombination || state.LastPlay.Seat < 0 || state.LastPlay.Seat > 2 {
			return nil, errors.New("invalid landlord last play")
		}
		played := [54]bool{}
		for _, card := range state.Played {
			played[card] = true
		}
		for _, card := range state.LastPlay.Cards {
			if !played[card] {
				return nil, errors.New("unplayed landlord last card")
			}
		}
		if state.Phase == "playing" && state.ToMove != (state.LastPlay.Seat+1+state.Passes)%3 {
			return nil, errors.New("invalid landlord trick turn")
		}
	} else if state.Passes != 0 {
		return nil, errors.New("invalid landlord passes")
	}
	var expectedPassed [3]bool
	if state.Passes == 1 {
		expectedPassed[(state.LastPlay.Seat+1)%3] = true
	}
	if state.Passed != expectedPassed {
		return nil, errors.New("invalid landlord passed seats")
	}
	return &state, nil
}

func landlordPvpState(state *landlordState) (pvpState, error) {
	data, err := common.Marshal(state)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: state.ToMove, Over: state.Phase == "finished", Winner: state.Winner}, nil
}

func (landlordEngine) New(first int) (pvpState, error) {
	if first < 0 || first > 2 {
		return pvpState{}, fmt.Errorf("invalid landlord first seat %d", first)
	}
	state := &landlordState{}
	if err := state.deal(first); err != nil {
		return pvpState{}, err
	}
	return landlordPvpState(state)
}

func (landlordEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	state, err := loadLandlord(data)
	if err != nil {
		return pvpState{}, err
	}
	if state.Phase == "finished" || seat != state.ToMove {
		return pvpState{}, errPvpIllegalMove
	}
	var req landlordMove
	if err := common.Unmarshal(move, &req); err != nil {
		return pvpState{}, errPvpIllegalMove
	}
	// 即使无效字段是 null，也拒绝一次请求同时提交不同种类的动作。
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(move, &fields); err != nil || len(fields) != 1 {
		return pvpState{}, errPvpIllegalMove
	}
	if state.Phase == "bidding" {
		if req.Bid == nil || req.Cards != nil || req.Pass != nil || *req.Bid < 0 || *req.Bid > 3 ||
			*req.Bid != 0 && *req.Bid <= state.HighBid {
			return pvpState{}, errPvpIllegalMove
		}
		state.Bids[seat] = *req.Bid
		state.BidCount++
		if *req.Bid > state.HighBid {
			state.HighBid, state.HighBidder = *req.Bid, seat
		}
		if *req.Bid == 3 || state.BidCount == 3 {
			if state.HighBid == 0 {
				if err := state.deal((state.BidFirst + 1) % 3); err != nil {
					return pvpState{}, err
				}
			} else {
				state.Phase, state.Landlord, state.ToMove = "playing", state.HighBidder, state.HighBidder
				state.Hands[state.Landlord] = append(state.Hands[state.Landlord], state.Bottom...)
				sort.Ints(state.Hands[state.Landlord])
			}
		} else {
			state.ToMove = (seat + 1) % 3
		}
		return landlordPvpState(state)
	}
	if req.Bid != nil {
		return pvpState{}, errPvpIllegalMove
	}
	if req.Pass != nil {
		if !*req.Pass || state.LastPlay == nil || state.LastPlay.Seat == seat || req.Cards != nil {
			return pvpState{}, errPvpIllegalMove
		}
		state.Passes++
		state.Passed[seat] = true
		state.ToMove = (seat + 1) % 3
		if state.Passes == 2 {
			state.ToMove = state.LastPlay.Seat
			state.LastPlay, state.Passes, state.Passed = nil, 0, [3]bool{}
		}
		return landlordPvpState(state)
	}
	combo, err := landlordClassify(req.Cards)
	if err != nil || state.LastPlay != nil && !combo.beats(state.LastPlay.landlordCombination) {
		return pvpState{}, errPvpIllegalMove
	}
	var inHand, chosen [54]bool
	for _, card := range state.Hands[seat] {
		inHand[card] = true
	}
	for _, card := range req.Cards {
		if !inHand[card] {
			return pvpState{}, errPvpIllegalMove
		}
		chosen[card] = true
	}
	hand := make([]int, 0, len(state.Hands[seat])-len(req.Cards))
	for _, card := range state.Hands[seat] {
		if !chosen[card] {
			hand = append(hand, card)
		}
	}
	sort.Ints(req.Cards)
	state.Hands[seat] = hand
	state.Played = append(state.Played, req.Cards...)
	state.LastPlay = &landlordPlay{landlordCombination: combo, Seat: seat, Cards: req.Cards}
	state.Passes, state.Passed = 0, [3]bool{}
	state.ToMove = (seat + 1) % 3
	if len(hand) == 0 {
		state.Phase, state.ToMove, state.Winner = "finished", -1, seat
	}
	return landlordPvpState(state)
}

func (landlordEngine) View(data []byte, seat int) (any, error) {
	state, err := loadLandlord(data)
	if err != nil {
		return nil, err
	}
	view := landlordView{
		Phase: state.Phase, Hand: []int{}, Bottom: []int{}, Bids: state.Bids, HighBid: state.HighBid,
		Deals: state.Deals, Landlord: state.Landlord, ToMove: state.ToMove, LastPlay: state.LastPlay,
		Passed: state.Passed, Over: state.Phase == "finished", Winner: state.Winner, Winners: []int{},
	}
	for member, hand := range state.Hands {
		view.Counts[member] = len(hand)
	}
	if seat >= 0 && seat < 3 {
		view.Hand = append(view.Hand, state.Hands[seat]...)
	}
	if state.Phase != "bidding" {
		view.Bottom = append(view.Bottom, state.Bottom...)
	}
	if view.Over {
		view.Hands = &state.Hands
		view.Winners, err = (landlordEngine{}).Winners(data, -1)
		if err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Winners 用自然胜负或认输/超时座位确定队伍，按座位升序返回。叫分时尚未组队，失败者之外的两人获胜。
func (landlordEngine) Winners(data []byte, loserSeat int) ([]int, error) {
	state, err := loadLandlord(data)
	if err != nil {
		return nil, err
	}
	if loserSeat < -1 || loserSeat > 2 || loserSeat == -1 && state.Phase != "finished" {
		return nil, errors.New("invalid landlord result")
	}
	if state.Landlord == -1 {
		winners := make([]int, 0, 2)
		for seat := 0; seat < 3; seat++ {
			if seat != loserSeat {
				winners = append(winners, seat)
			}
		}
		return winners, nil
	}
	landlordWon := state.Winner == state.Landlord
	if loserSeat >= 0 {
		landlordWon = loserSeat != state.Landlord
	}
	if landlordWon {
		return []int{state.Landlord}, nil
	}
	winners := make([]int, 0, 2)
	for seat := 0; seat < 3; seat++ {
		if seat != state.Landlord {
			winners = append(winners, seat)
		}
	}
	return winners, nil
}

// Timeout 保留原双人接口的代表赢家；多人结算由框架调用 Winners 并传入实际超时座位。
func (landlordEngine) Timeout(data []byte) (int, error) {
	state, err := loadLandlord(data)
	if err != nil {
		return -1, err
	}
	loser := state.ToMove
	if state.Phase == "finished" {
		loser = -1
	}
	winners, err := (landlordEngine{}).Winners(data, loser)
	if err != nil {
		return -1, err
	}
	return winners[0], nil
}

func (landlordEngine) AiMove([]byte, int, int) (json.RawMessage, error) {
	return nil, errors.New("landlord has no ai opponent")
}
