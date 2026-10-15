package controller

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/landlord"
)

// 斗地主对战：三人桌，规则与 AI 见 pkg/landlord。押注是底注：地主输赢两倍、两位农民各输赢一倍，叫分和炸弹不加倍。
// 房间里超时的玩家由 AI 托管代打，不判他所在的队伍输；人机对战是玩家坐座位 0，另两个座位都是 AI。
const gameLandlord = "landlord"

func init() {
	registerPvpGame(gameLandlord, "斗地主", landlordEngine{})
}

type landlordEngine struct{}

// landlordMove 是一步：{"bid":0-3} 叫分(0 是不叫)、{"cards":[牌号…]} 出牌或 {"pass":true} 不出，一次只能是其中一种。
type landlordMove struct {
	Bid   *int  `json:"bid,omitempty"`
	Cards []int `json:"cards,omitempty"`
	Pass  *bool `json:"pass,omitempty"`
}

type landlordView struct {
	landlord.View
	Hints [][]int `json:"hints"`
}

// loadLandlord 读出保存的一局并检查前后一致，损坏的牌局不能继续出牌，也不能拿去结算。
func loadLandlord(data []byte) (*landlord.Game, error) {
	var game landlord.Game
	if err := common.Unmarshal(data, &game); err != nil {
		return nil, err
	}
	if err := game.Validate(); err != nil {
		return nil, err
	}
	return &game, nil
}

func landlordPvpState(game *landlord.Game) (pvpState, error) {
	data, err := common.Marshal(game)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: game.ToMove, Over: game.Over(), Winner: game.Winner}, nil
}

func (landlordEngine) Seats() int { return landlord.Players }

func (landlordEngine) New(first int) (pvpState, error) {
	game, err := landlord.New(first)
	if err != nil {
		return pvpState{}, err
	}
	return landlordPvpState(game)
}

func (landlordEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	game, err := loadLandlord(data)
	if err != nil {
		return pvpState{}, err
	}
	var req landlordMove
	var fields map[string]json.RawMessage
	// 一次只能提交一种动作，哪怕另外的字段是 null。
	if common.Unmarshal(move, &req) != nil || common.Unmarshal(move, &fields) != nil || len(fields) != 1 {
		return pvpState{}, errPvpIllegalMove
	}
	switch {
	case req.Bid != nil:
		err = game.Bid(seat, *req.Bid)
	case req.Pass != nil && *req.Pass:
		err = game.Pass(seat)
	case req.Cards != nil:
		err = game.Play(seat, req.Cards)
	default:
		err = landlord.ErrIllegalMove
	}
	if errors.Is(err, landlord.ErrIllegalMove) {
		return pvpState{}, errPvpIllegalMove
	}
	if err != nil {
		return pvpState{}, err
	}
	return landlordPvpState(game)
}

func (landlordEngine) View(data []byte, seat int) (any, error) {
	game, err := loadLandlord(data)
	if err != nil {
		return nil, err
	}
	view := landlordView{View: game.View(seat), Hints: [][]int{}}
	if game.Phase == landlord.PhasePlaying && !game.Over() && seat >= 0 && seat < landlord.Players && game.ToMove == seat {
		view.Hints = landlord.HintMoves(game.Knowledge(seat))
	}
	return view, nil
}

// MaxLoss 是两倍：地主最多输两倍底注，而谁当地主要叫完分才知道，所以每人入座时都押两倍，农民结算时退回没输掉的一倍。
func (landlordEngine) MaxLoss() int { return 2 }

func (landlordEngine) Scores(data []byte) ([]int, error) {
	game, err := loadLandlord(data)
	if err != nil {
		return nil, err
	}
	if !game.Over() {
		return nil, errors.New("landlord game is not over")
	}
	return game.Scores(), nil
}

// AiMove 替 seat 选一步。AI 只拿到这个座位能看到的信息，托管与人机对战都不会偷看别人的牌。
func (landlordEngine) AiMove(data []byte, seat int, level int) (json.RawMessage, error) {
	game, err := loadLandlord(data)
	if err != nil {
		return nil, err
	}
	if game.ToMove != seat {
		return nil, errPvpIllegalMove
	}
	rng, err := boardAiRandom()
	if err != nil {
		return nil, err
	}
	move, err := landlord.BestMove(game.Knowledge(seat), level, rng)
	if err != nil {
		return nil, err
	}
	pass := true
	switch {
	case move.Bid != nil:
		return common.Marshal(landlordMove{Bid: move.Bid})
	case move.Pass:
		return common.Marshal(landlordMove{Pass: &pass})
	}
	return common.Marshal(landlordMove{Cards: move.Cards})
}
