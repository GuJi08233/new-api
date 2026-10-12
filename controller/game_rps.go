package controller

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/rps"
)

const gameRps = "rps"

func init() {
	registerPvpGame(gameRps, "石头剪刀布", rpsEngine{})
}

// rpsEngine 是石头剪刀布的对战规则引擎，规则在 pkg/rps。双方同时出拳，没有先后之分，也没有人机对战。
type rpsEngine struct{}

// rpsMove 是一次出拳在接口里的样子：{"choice":"rock|paper|scissors","round":n}。round 是这一拳出在第几回合(见 View
// 的 round)：双方同时出拳，落子不核对房间版本，靠它避免迟到的一拳落进下一回合。
type rpsMove struct {
	Choice rps.Choice `json:"choice"`
	Round  int        `json:"round"`
}

var errRpsNoAi = errors.New("rps has no ai opponent")

func loadRpsMatch(data []byte) (*rps.Match, error) {
	var match rps.Match
	if err := common.Unmarshal(data, &match); err != nil {
		return nil, err
	}
	return &match, nil
}

// rpsPvpState 把比赛交给框架：一方出了拳就轮到另一方(它的限时重新开始)，双方都没出时 ToMove 为 -1。
func rpsPvpState(match *rps.Match) (pvpState, error) {
	data, err := common.Marshal(match)
	if err != nil {
		return pvpState{}, err
	}
	return pvpState{Data: data, ToMove: match.ToMove(), Over: match.Over, Winner: match.Winner}, nil
}

func (rpsEngine) simultaneous() {}

// New 开一场比赛。双方同时出拳，先走的座位没有意义。
func (rpsEngine) New(int) (pvpState, error) {
	match := rps.New()
	return rpsPvpState(&match)
}

func (rpsEngine) Play(data []byte, seat int, move json.RawMessage) (pvpState, error) {
	match, err := loadRpsMatch(data)
	if err != nil {
		return pvpState{}, err
	}
	var req rpsMove
	if err := common.Unmarshal(move, &req); err != nil {
		return pvpState{}, errPvpIllegalMove
	}
	if req.Round != len(match.Rounds)+1 {
		return pvpState{}, model.ErrGameRoomChanged
	}
	if err := match.Play(seat, req.Choice); err != nil {
		return pvpState{}, errPvpIllegalMove
	}
	return rpsPvpState(match)
}

// View 只给看的人他自己这一回合出的拳，对手出了什么在这一回合公开之前绝不返回。
func (rpsEngine) View(data []byte, seat int) (any, error) {
	match, err := loadRpsMatch(data)
	if err != nil {
		return nil, err
	}
	return match.View(seat), nil
}

// Timeout 在这一回合双方都没出时判和，否则判还没出的一方输。
func (rpsEngine) Timeout(data []byte) (int, error) {
	match, err := loadRpsMatch(data)
	if err != nil {
		return -1, err
	}
	return match.TimeoutWinner(), nil
}

// AiMove 不支持：石头剪刀布没有人机对战，配置里也不能打开。
func (rpsEngine) AiMove([]byte, int, int) (json.RawMessage, error) {
	return nil, errRpsNoAi
}
