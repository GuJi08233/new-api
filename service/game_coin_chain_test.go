package service

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 与 eth_account(web3.py 7.16)对同一把私钥签出的结果逐字节比对。签名是 RFC6979
// 确定性的，任何 RLP 字段顺序、长度前缀或 yParity 编码的偏差都会让原始交易对不上。
const gameCoinTestKey = "0x4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"

func TestParseGameHotWalletDerivesReferenceAddress(t *testing.T) {
	wallet, err := parseGameHotWallet(gameCoinTestKey)
	require.NoError(t, err)
	assert.Equal(t, "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23", wallet.address)

	for _, key := range []string{
		"",
		"0x1234",
		"0x" + strings.Repeat("00", 32),
		// 等于曲线阶 N 的值取模后为 0；N+1 会被静默取模成私钥 1，派生出运营方不认识的地址。
		"0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141",
		"0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364142",
	} {
		_, err := parseGameHotWallet(key)
		assert.Error(t, err, key)
	}
}

// 管理员在后台填写的私钥优先于环境变量；清除后台私钥后回落到环境变量，两处都没有时报未配置。
func TestGameHotWalletPrefersAdminKeyOverEnv(t *testing.T) {
	previousKey := operation_setting.GetGameHotWalletPrivateKey()
	t.Cleanup(func() { operation_setting.SetGameHotWalletPrivateKey(previousKey) })
	// 私钥 1 对应的地址是公认的参考值。
	t.Setenv(GameHotWalletPrivateKeyEnv, "0x0000000000000000000000000000000000000000000000000000000000000001")

	operation_setting.SetGameHotWalletPrivateKey(strings.TrimPrefix(gameCoinTestKey, "0x"))
	address, source, err := GameHotWalletAddress()
	require.NoError(t, err)
	assert.Equal(t, "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23", address)
	assert.Equal(t, GameHotWalletSourceSetting, source)

	operation_setting.SetGameHotWalletPrivateKey("")
	address, source, err = GameHotWalletAddress()
	require.NoError(t, err)
	assert.Equal(t, "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf", address)
	assert.Equal(t, GameHotWalletSourceEnv, source)

	t.Setenv(GameHotWalletPrivateKeyEnv, "")
	_, source, err = GameHotWalletAddress()
	assert.ErrorIs(t, err, ErrGameHotWalletMissing)
	assert.Empty(t, source)
}

func TestSignGameTransferTxMatchesReferenceSigner(t *testing.T) {
	wallet, err := parseGameHotWallet(gameCoinTestKey)
	require.NoError(t, err)
	token, err := ethereumAddressBytes("0x64bf89340562AB4fE829a481E6797B012664425B")
	require.NoError(t, err)
	recipient, err := ethereumAddressBytes("0x8ba1f109551bD432803012645Ac136ddd64DBA72")
	require.NoError(t, err)

	cases := []struct {
		nonce  uint64
		amount int64
		tip    int64
		maxFee int64
		gas    uint64
		rawTx  string
		txHash string
	}{
		{
			nonce: 7, amount: 5, tip: 1500000000, maxFee: 30000000000, gas: 60000,
			rawTx:  "0x02f8b383aa36a7078459682f008506fc23ac0082ea609464bf89340562ab4fe829a481e6797b012664425b80b844a9059cbb0000000000000000000000008ba1f109551bd432803012645ac136ddd64dba720000000000000000000000000000000000000000000000000000000000000005c080a0b7df58478734ce7fceb53cacb9e336c433f102ef41c8cf562ce1ccc2e6b65462a0125b26047b4fba30ce7762aaf5a5046c526e1366317bccb15ceb279ff0187369",
			txHash: "0x9cb4b054ad20187b95f8757a4bb3c34604e57a1b9208ccfc70b5f32b01302372",
		},
		{
			// nonce 0 编码成空串，1 字节以内的小整数编码成自身。
			nonce: 0, amount: 123456789, tip: 1, maxFee: 2, gas: 21000,
			rawTx:  "0x02f8aa83aa36a78001028252089464bf89340562ab4fe829a481e6797b012664425b80b844a9059cbb0000000000000000000000008ba1f109551bd432803012645ac136ddd64dba7200000000000000000000000000000000000000000000000000000000075bcd15c080a0aac1998eb6c6cb35410b8cf468be15ae306aba8cfea575cee6082eb4cecafefba0646b0e0e7a3906fa918a381c3172474364aa0cc174842ea9d9490a3637edf81f",
			txHash: "0xd20574759b63197ce4f241f2dc4ef985778fc1602fa6eb8940400ea931c61634",
		},
	}
	for _, tc := range cases {
		tx := &gameTransferTx{
			chainId:              big.NewInt(11155111),
			nonce:                tc.nonce,
			maxPriorityFeePerGas: big.NewInt(tc.tip),
			maxFeePerGas:         big.NewInt(tc.maxFee),
			gas:                  tc.gas,
			to:                   token,
			data:                 erc20TransferData(recipient, big.NewInt(tc.amount)),
		}
		rawTx, txHash, err := signGameTransferTx(tx, wallet.key)
		require.NoError(t, err)
		assert.Equal(t, tc.rawTx, rawTx)
		assert.Equal(t, tc.txHash, txHash)
	}
}

func TestNormalizeEthereumAddressEnforcesChecksum(t *testing.T) {
	cases := map[string]string{
		"0x8ba1f109551bd432803012645ac136ddd64dba72": "0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		"0x8BA1F109551BD432803012645AC136DDD64DBA72": "0x8ba1f109551bD432803012645Ac136ddd64DBA72",
		"0x8ba1f109551bD432803012645Ac136ddd64DBA72": "0x8ba1f109551bD432803012645Ac136ddd64DBA72",
	}
	for input, expected := range cases {
		normalized, err := NormalizeEthereumAddress(input)
		require.NoError(t, err, input)
		assert.Equal(t, expected, normalized)
	}

	for _, input := range []string{
		"0x8ba1f109551bD432803012645Ac136ddd64DBa72", // 混排但校验不符，多半是抄错了
		"0x8ba1f109551bd432803012645ac136ddd64dba7",
		"8ba1f109551bd432803012645ac136ddd64dba7200",
		"0xzba1f109551bd432803012645ac136ddd64dba72",
	} {
		_, err := NormalizeEthereumAddress(input)
		assert.Error(t, err, input)
	}
}

func TestGameTokenRawAmountScalesByDecimals(t *testing.T) {
	assert.Equal(t, "7", GameTokenRawAmount(7, 0).String())
	assert.Equal(t, "7000000000000000000", GameTokenRawAmount(7, 18).String())

	recipient, err := ethereumAddressBytes("0x8ba1f109551bD432803012645Ac136ddd64DBA72")
	require.NoError(t, err)
	assert.Equal(t,
		"a9059cbb0000000000000000000000008ba1f109551bd432803012645ac136ddd64dba720000000000000000000000000000000000000000000000000000000000000005",
		hex.EncodeToString(erc20TransferData(recipient, GameTokenRawAmount(5, 0))))
}
