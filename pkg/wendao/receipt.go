package wendao

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// 请求收据：页面每次操作带一个请求编号(request_id)，改写存档时把编号和请求内容的摘要一起记进这份存档，和这次操作的
// 改动在同一个事务里保存。网络断了、页面没收到回应，拿同一个编号重发时，认出是做过的那一次，不再做第二遍(不重复扣料、
// 祭炼、购买或领奖)，交回现在的样子；编号相同而内容不同说明页面出了错，拒绝。操作本身失败时整次改写不保存，收据也就
// 没记下，重发会照常再试。只记最近 ReceiptKeep 张、ReceiptTTL 秒以内的；没带编号的请求(旧页面)照常执行，不记。
const (
	ReceiptKeep = 32
	ReceiptTTL  = 24 * 3600
	// RequestIdMaxLen 是请求编号最长多少个字节，页面用的是 36 个字符的 UUID。
	RequestIdMaxLen = 64
)

// Receipt 是一次做过的操作：请求编号、请求内容的摘要与时间。
type Receipt struct {
	Id  string `json:"id"`
	Sum string `json:"sum"`
	At  int64  `json:"at"`
}

var (
	// ErrReplayed 表示这次请求已经做过了，调用方不保存这次改写，交回现在的样子。
	ErrReplayed = errors.New("wendao: the request was already done")
	// ErrRequestReused 表示请求编号已经用在另一个内容不同的请求上，或者编号太长。
	ErrRequestReused = errors.New("wendao: the request id belongs to a different request")
)

// RequestSum 是请求内容的摘要，调用方先把请求里的编号清空再传进来。
func RequestSum(request any) (string, error) {
	data, err := common.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16]), nil
}

// Admit 在执行一次操作之前调用：做过的同一次请求返回 ErrReplayed，编号相同而内容不同返回 ErrRequestReused，否则记下这张
// 收据。id 为空时什么都不做。顺手丢掉过期的收据，超过 ReceiptKeep 张时丢掉最早的。
func (s *Save) Admit(id, sum string, now int64) error {
	if id == "" {
		return nil
	}
	if len(id) > RequestIdMaxLen {
		return ErrRequestReused
	}
	kept := s.Receipts[:0]
	for _, r := range s.Receipts {
		if now-r.At < ReceiptTTL {
			kept = append(kept, r)
		}
	}
	s.Receipts = kept
	for _, r := range s.Receipts {
		if r.Id != id {
			continue
		}
		if r.Sum == sum {
			return ErrReplayed
		}
		return ErrRequestReused
	}
	s.Receipts = append(s.Receipts, Receipt{Id: id, Sum: sum, At: now})
	if extra := len(s.Receipts) - ReceiptKeep; extra > 0 {
		s.Receipts = s.Receipts[extra:]
	}
	return nil
}
