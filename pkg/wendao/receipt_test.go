package wendao

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 请求收据：同一次请求重发认得出来，编号被拿去装别的内容要拒绝；过期与超出张数的收据丢掉；收据随存档保存下来。
func TestAdmitRemembersRequests(t *testing.T) {
	const now = int64(1_800_000_000)
	save := &Save{}
	buy, err := RequestSum(Action{Type: "buy", Item: "qi", Count: 2})
	require.NoError(t, err)
	other, err := RequestSum(Action{Type: "buy", Item: "qi", Count: 3})
	require.NoError(t, err)
	assert.NotEqual(t, buy, other)

	require.NoError(t, save.Admit("", buy, now), "requests without an id are never remembered")
	assert.Empty(t, save.Receipts)

	require.NoError(t, save.Admit("a", buy, now))
	assert.ErrorIs(t, save.Admit("a", buy, now+5), ErrReplayed)
	assert.ErrorIs(t, save.Admit("a", other, now+5), ErrRequestReused)
	require.NoError(t, save.Admit("b", buy, now+5), "a new id for the same content is a new request")
	assert.ErrorIs(t, save.Admit(string(make([]byte, RequestIdMaxLen+1)), buy, now), ErrRequestReused)

	data, err := save.Encode()
	require.NoError(t, err)
	parsed, err := ParseSave(data)
	require.NoError(t, err)
	assert.ErrorIs(t, parsed.Admit("a", buy, now+10), ErrReplayed, "receipts survive saving and loading")

	require.NoError(t, parsed.Admit("a", buy, now+ReceiptTTL), "an expired receipt is forgotten")
	assert.Equal(t, []string{"b", "a"}, receiptIds(parsed), "b is five seconds younger and still kept")

	for i := range ReceiptKeep + 3 {
		require.NoError(t, parsed.Admit(fmt.Sprintf("r%d", i), buy, now+ReceiptTTL+int64(i)))
	}
	ids := receiptIds(parsed)
	assert.Len(t, ids, ReceiptKeep)
	assert.Equal(t, "r3", ids[0], "the oldest receipts go first")
	require.NoError(t, parsed.Admit("a", buy, now+ReceiptTTL+100), "a dropped receipt no longer blocks its id")
}

func receiptIds(save *Save) []string {
	ids := make([]string, 0, len(save.Receipts))
	for _, r := range save.Receipts {
		ids = append(ids, r.Id)
	}
	return ids
}
