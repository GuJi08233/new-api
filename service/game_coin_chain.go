package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// GameHotWalletPrivateKeyEnv 是热钱包私钥的备用来源。管理员在游戏管理页填写的私钥优先，
// 没有填写时才读这个环境变量，留给不想把私钥存进数据库的部署。
const GameHotWalletPrivateKeyEnv = "GAME_HOT_WALLET_PRIVATE_KEY"

// 热钱包私钥的来源，显示在管理端：清除后台私钥之后是否还有环境变量兜底，一眼可见。
const (
	GameHotWalletSourceSetting = "setting"
	GameHotWalletSourceEnv     = "env"
)

var ErrGameHotWalletMissing = errors.New("hot wallet private key is not configured")

// gameHotWallet 是发放游戏币代币的专用账户。它只做 ERC-20 transfer，
// 不需要是代币合约的 owner，里面也只应放当日周转所需的代币和 gas。
type gameHotWallet struct {
	key     *secp256k1.PrivateKey
	address string // EIP-55 格式
}

// gameHotWalletKey 返回当前生效的私钥与来源：后台填写的优先，其次是环境变量。每次调用都现读本节点的配置，
// 不需要重启；多节点部署时别的节点要等同步配置后才看到新私钥，提现 worker 签名前会先从数据库现读。
func gameHotWalletKey() (key string, source string) {
	if key := operation_setting.GetGameHotWalletPrivateKey(); key != "" {
		return key, GameHotWalletSourceSetting
	}
	if key := strings.TrimSpace(os.Getenv(GameHotWalletPrivateKeyEnv)); key != "" {
		return key, GameHotWalletSourceEnv
	}
	return "", ""
}

func loadGameHotWallet() (*gameHotWallet, error) {
	key, _ := gameHotWalletKey()
	return parseGameHotWallet(key)
}

func parseGameHotWallet(hexKey string) (*gameHotWallet, error) {
	hexKey = strings.TrimPrefix(strings.TrimSpace(hexKey), "0x")
	if hexKey == "" {
		return nil, ErrGameHotWalletMissing
	}
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("hot wallet private key must be 32 bytes of hex")
	}
	var scalar secp256k1.ModNScalar
	// 大于等于曲线阶的值会被静默取模成另一把私钥，派生出运营方不认识的地址，必须拒绝。
	if overflow := scalar.SetByteSlice(raw); overflow || scalar.IsZero() {
		return nil, errors.New("hot wallet private key is not a valid secp256k1 private key")
	}
	key := secp256k1.NewPrivateKey(&scalar)
	return &gameHotWallet{key: key, address: ethereumAddressFromPublicKey(key.PubKey())}, nil
}

// GameHotWalletAddress 返回当前生效热钱包的地址与私钥来源。私钥无效时仍给出来源，
// 管理端据此指出填错的是后台私钥还是环境变量。
func GameHotWalletAddress() (address string, source string, err error) {
	key, source := gameHotWalletKey()
	wallet, err := parseGameHotWallet(key)
	if err != nil {
		return "", source, err
	}
	return wallet.address, source, nil
}

// GameHotWalletAddressOf 校验一把私钥并返回它对应的地址，管理员保存私钥前据此确认填的是哪个钱包。
func GameHotWalletAddressOf(hexKey string) (string, error) {
	wallet, err := parseGameHotWallet(hexKey)
	if err != nil {
		return "", err
	}
	return wallet.address, nil
}

func keccak256(data ...[]byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	for _, chunk := range data {
		hasher.Write(chunk)
	}
	return hasher.Sum(nil)
}

func ethereumAddressFromPublicKey(pub *secp256k1.PublicKey) string {
	uncompressed := pub.SerializeUncompressed()
	return checksumEthereumAddress(keccak256(uncompressed[1:])[12:])
}

// checksumEthereumAddress 按 EIP-55 给地址的字母位加上大小写校验。
func checksumEthereumAddress(address []byte) string {
	lower := hex.EncodeToString(address)
	hash := keccak256([]byte(lower))
	var builder strings.Builder
	builder.WriteString("0x")
	for i, char := range lower {
		nibble := hash[i/2]
		if i%2 == 0 {
			nibble >>= 4
		}
		if char >= 'a' && char <= 'f' && nibble&0x0f >= 8 {
			char -= 'a' - 'A'
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

// NormalizeEthereumAddress 校验地址并返回 EIP-55 格式。全小写或全大写的地址不带校验信息，
// 照常接受；大小写混排的地址必须通过 EIP-55 校验，抄错一位的地址会在这里被拦下。
func NormalizeEthereumAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return "", errors.New("address must be 0x followed by 40 hex characters")
	}
	body := address[2:]
	raw, err := hex.DecodeString(body)
	if err != nil {
		return "", errors.New("address must be 0x followed by 40 hex characters")
	}
	checksummed := checksumEthereumAddress(raw)
	mixedCase := body != strings.ToLower(body) && body != strings.ToUpper(body)
	if mixedCase && checksummed[2:] != body {
		return "", errors.New("address checksum mismatch")
	}
	return checksummed, nil
}

func ethereumAddressBytes(address string) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(address, "0x"))
	if err != nil || len(raw) != 20 {
		return nil, fmt.Errorf("invalid address %q", address)
	}
	return raw, nil
}

// GameTokenRawAmount 把游戏币数量换算成代币合约的最小单位：数量 × 10^decimals。
func GameTokenRawAmount(coins int, decimals int) *big.Int {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	return scale.Mul(scale, big.NewInt(int64(coins)))
}

// erc20TransferData 编码 transfer(address,uint256) 的调用数据。
func erc20TransferData(to []byte, amount *big.Int) []byte {
	data := make([]byte, 4+64)
	copy(data, []byte{0xa9, 0x05, 0x9c, 0xbb})
	copy(data[4+12:4+32], to)
	amount.FillBytes(data[4+32:])
	return data
}

// erc20BalanceOfData 编码 balanceOf(address) 的调用数据。
func erc20BalanceOfData(owner []byte) []byte {
	data := make([]byte, 4+32)
	copy(data, []byte{0x70, 0xa0, 0x82, 0x31})
	copy(data[4+12:], owner)
	return data
}

// RLP 编码只覆盖一笔 EIP-1559 转账用到的两种形态：字节串与列表，整数按无前导零的大端字节串编码。
func rlpEncodeBytes(data []byte) []byte {
	if len(data) == 1 && data[0] < 0x80 {
		return []byte{data[0]}
	}
	return append(rlpLengthPrefix(0x80, len(data)), data...)
}

func rlpEncodeUint(value *big.Int) []byte {
	return rlpEncodeBytes(value.Bytes())
}

func rlpEncodeList(items ...[]byte) []byte {
	payload := bytes.Join(items, nil)
	return append(rlpLengthPrefix(0xc0, len(payload)), payload...)
}

func rlpLengthPrefix(offset byte, length int) []byte {
	if length <= 55 {
		return []byte{offset + byte(length)}
	}
	lengthBytes := big.NewInt(int64(length)).Bytes()
	return append([]byte{offset + 55 + byte(len(lengthBytes))}, lengthBytes...)
}

// gameTransferTx 是一笔不带 ETH 的 EIP-1559 合约调用。
type gameTransferTx struct {
	chainId              *big.Int
	nonce                uint64
	maxPriorityFeePerGas *big.Int
	maxFeePerGas         *big.Int
	gas                  uint64
	to                   []byte
	data                 []byte
}

func (tx *gameTransferTx) rlpFields() [][]byte {
	return [][]byte{
		rlpEncodeUint(tx.chainId),
		rlpEncodeUint(new(big.Int).SetUint64(tx.nonce)),
		rlpEncodeUint(tx.maxPriorityFeePerGas),
		rlpEncodeUint(tx.maxFeePerGas),
		rlpEncodeUint(new(big.Int).SetUint64(tx.gas)),
		rlpEncodeBytes(tx.to),
		rlpEncodeUint(big.NewInt(0)),
		rlpEncodeBytes(tx.data),
		rlpEncodeList(),
	}
}

// signGameTransferTx 按 EIP-1559 签名，返回可直接 eth_sendRawTransaction 的原始交易与交易哈希。
// 签名是 RFC6979 确定性的低 S 值签名，同一笔交易重复签名得到同一个哈希。
func signGameTransferTx(tx *gameTransferTx, key *secp256k1.PrivateKey) (rawTx string, txHash string, err error) {
	unsigned := append([]byte{0x02}, rlpEncodeList(tx.rlpFields()...)...)
	signature := ecdsa.SignCompact(key, keccak256(unsigned), false)
	recoveryId := signature[0] - 27
	if recoveryId > 1 {
		// 只有 R 超过曲线阶时才会出现，以太坊的 yParity 无法表示。
		return "", "", errors.New("signature recovery id out of range")
	}
	fields := append(tx.rlpFields(),
		rlpEncodeUint(big.NewInt(int64(recoveryId))),
		rlpEncodeUint(new(big.Int).SetBytes(signature[1:33])),
		rlpEncodeUint(new(big.Int).SetBytes(signature[33:65])),
	)
	raw := append([]byte{0x02}, rlpEncodeList(fields...)...)
	return "0x" + hex.EncodeToString(raw), "0x" + hex.EncodeToString(keccak256(raw)), nil
}

// parseEthereumQuantity 解析 RPC 返回的十六进制数值，兼容带前导零的 DATA 字段。空值(字段为 null、没有返回值，
// 或者只有 "0x")一律报错：零要写成 "0x0"。把空值当成 0 的话，还没打包的回执会被当成第 0 个区块里执行失败的交易，
// 空的 gas 预估会签出一笔 gas 为 0 的交易。
func parseEthereumQuantity(value string) (*big.Int, error) {
	clean := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if clean == "" {
		return nil, fmt.Errorf("missing quantity %q", value)
	}
	parsed, ok := new(big.Int).SetString(clean, 16)
	if !ok {
		return nil, fmt.Errorf("malformed quantity %q", value)
	}
	return parsed, nil
}

func callEthereumRpcQuantity(ctx context.Context, rpcURL string, method string, params []any) (*big.Int, error) {
	var value string
	if err := callEthereumRpc(ctx, rpcURL, method, params, &value); err != nil {
		return nil, err
	}
	return parseEthereumQuantity(value)
}

// GameHotWalletBalances 查询热钱包的代币余额(合约最小单位)与 ETH 余额(wei)，供管理端展示。
func GameHotWalletBalances(ctx context.Context, setting *operation_setting.GameSetting) (token *big.Int, eth *big.Int, err error) {
	wallet, err := loadGameHotWallet()
	if err != nil {
		return nil, nil, err
	}
	owner, err := ethereumAddressBytes(wallet.address)
	if err != nil {
		return nil, nil, err
	}
	call := map[string]any{
		"to":   setting.TokenAddress,
		"data": "0x" + hex.EncodeToString(erc20BalanceOfData(owner)),
	}
	token, err = callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_call", []any{call, "latest"})
	if err != nil {
		return nil, nil, err
	}
	eth, err = callEthereumRpcQuantity(ctx, setting.RpcUrl, "eth_getBalance", []any{wallet.address, "latest"})
	if err != nil {
		return nil, nil, err
	}
	return token, eth, nil
}
