package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 游戏币是与主钱包 quota 相互独立的余额：由游戏发放或管理员调整获得，可以单向兑换成
// 主额度(1 币 = 1 美元额度)，也可以提现成链上 ERC-20 代币。余额、流水和提现单都在主库，
// 兑换时扣币与 quota 入账在同一个事务里；日志库只做事后审计，不参与记账。

const (
	GameCoinLogTypeAdminAdjust    = "admin_adjust"
	GameCoinLogTypeConvert        = "convert"
	GameCoinLogTypeWithdraw       = "withdraw"
	GameCoinLogTypeWithdrawRefund = "withdraw_refund"
	GameCoinLogTypeGameReward     = "game_reward"
)

// 提现单状态。pending 到 signed 之间还没有任何链上动作；一旦 signed，签好的交易就已落库，
// 之后只能重发同一笔交易，直到链上给出结论，绝不能因为广播报错就退款——节点可能已经收下了它。
const (
	GameCoinWithdrawalStatusPending   = "pending"   // 已扣币并占用当日额度，等待签名
	GameCoinWithdrawalStatusSigned    = "signed"    // 交易已签名落库，节点尚未确认接收
	GameCoinWithdrawalStatusSent      = "sent"      // 节点已接收，等待上链与确认数
	GameCoinWithdrawalStatusConfirmed = "confirmed" // 已上链并达到确认数
	GameCoinWithdrawalStatusFailed    = "failed"    // 未能转出，游戏币已退回、当日额度已释放
)

var (
	ErrGameCoinAmountInvalid = errors.New("game coin amount is out of range")
	ErrGameCoinInsufficient  = errors.New("game coin balance is insufficient")
	ErrGameCoinDailyLimit    = errors.New("game coin daily withdrawal limit reached")
	ErrGameCoinUserNotFound  = errors.New("user not found")
)

var unfinishedGameCoinWithdrawalStatuses = []string{
	GameCoinWithdrawalStatusPending,
	GameCoinWithdrawalStatusSigned,
	GameCoinWithdrawalStatusSent,
}

// GameCoinAccount 以 user_id 为主键，首次入账时创建。
type GameCoinAccount struct {
	UserId         int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"`
	Balance        int   `json:"balance" gorm:"type:bigint;not null;default:0"`
	TotalEarned    int   `json:"total_earned" gorm:"type:bigint;not null;default:0"`
	TotalConverted int   `json:"total_converted" gorm:"type:bigint;not null;default:0"`
	TotalWithdrawn int   `json:"total_withdrawn" gorm:"type:bigint;not null;default:0"`
	CreatedAt      int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt      int64 `json:"updated_at" gorm:"bigint"`
}

// GameCoinLog 是游戏币流水，每次余额变动一条。
type GameCoinLog struct {
	Id        int    `json:"id"`
	UserId    int    `json:"user_id" gorm:"index"`
	Type      string `json:"type" gorm:"type:varchar(32)"`
	Amount    int    `json:"amount" gorm:"type:bigint"`  // 入账为正，出账为负
	Balance   int    `json:"balance" gorm:"type:bigint"` // 本次变动后的余额
	Quota     int    `json:"quota" gorm:"type:bigint"`   // 兑换得到的主额度，只有兑换流水有值
	RefId     string `json:"ref_id" gorm:"type:varchar(64);index"`
	Remark    string `json:"remark" gorm:"type:varchar(255)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

// GameCoinWithdrawal 是一笔链上提现。签名时把链、合约和发送地址一起记下：
// 之后即使管理员换了配置或热钱包，对账仍按这笔交易当时的上下文进行。
type GameCoinWithdrawal struct {
	Id           int    `json:"id"`
	TradeNo      string `json:"trade_no" gorm:"type:varchar(64);uniqueIndex"`
	UserId       int    `json:"user_id" gorm:"index"`
	ToAddress    string `json:"to_address" gorm:"type:varchar(42)"`
	Amount       int    `json:"amount" gorm:"type:bigint"`
	Day          string `json:"day" gorm:"type:varchar(10);index"`
	Status       string `json:"status" gorm:"type:varchar(16);index"`
	ChainId      int64  `json:"chain_id" gorm:"bigint"`
	TokenAddress string `json:"token_address" gorm:"type:varchar(42)"`
	FromAddress  string `json:"from_address" gorm:"type:varchar(42);index"`
	Nonce        int64  `json:"nonce" gorm:"bigint"`
	TxHash       string `json:"tx_hash" gorm:"type:varchar(66);index"`
	RawTx        string `json:"-" gorm:"type:text"`
	FailReason   string `json:"fail_reason" gorm:"type:varchar(255)"`
	// Error 是最近一次链上操作的原始错误，可能带有 RPC 地址，只给管理员看。
	Error       string `json:"-" gorm:"type:varchar(512)"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint"`
	BroadcastAt int64  `json:"broadcast_at" gorm:"bigint"`
	CompletedAt int64  `json:"completed_at" gorm:"bigint"`
}

// GameCoinWithdrawDay 记录全站每天已占用的提现额度。额度用条件更新扣减，
// 多实例并发提现也不会越过上限；失败退回时同步释放。
type GameCoinWithdrawDay struct {
	Day  string `json:"day" gorm:"type:varchar(10);primaryKey"`
	Used int    `json:"used" gorm:"type:bigint;not null;default:0"`
}

func GetGameCoinAccount(userId int) (GameCoinAccount, error) {
	var accounts []GameCoinAccount
	if err := DB.Where("user_id = ?", userId).Limit(1).Find(&accounts).Error; err != nil {
		return GameCoinAccount{}, err
	}
	if len(accounts) == 0 {
		return GameCoinAccount{UserId: userId}, nil
	}
	return accounts[0], nil
}

// creditGameCoinsTx 给账户加币，账户不存在时先建出来。余额和 quota 一样受 2^53 边界约束，
// 越界时返回错误让整笔事务回滚，而不是写出前端无法精确表示的数字。
func creditGameCoinsTx(tx *gorm.DB, userId int, amount int, fields map[string]interface{}) (int, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	account := &GameCoinAccount{UserId: userId, CreatedAt: now, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(account).Error; err != nil {
		return 0, err
	}
	updates := map[string]interface{}{
		"balance":    gorm.Expr("balance + ?", amount),
		"updated_at": now,
	}
	for key, value := range fields {
		updates[key] = value
	}
	result := tx.Model(&GameCoinAccount{}).Where("user_id = ? AND balance <= ?", userId, common.MaxQuota-1-amount).Updates(updates)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, ErrGameCoinAmountInvalid
	}
	return readGameCoinBalanceTx(tx, userId)
}

// debitGameCoinsTx 用一条条件更新检查并扣减余额，并发扣减不会透支。
func debitGameCoinsTx(tx *gorm.DB, userId int, amount int, fields map[string]interface{}) (int, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	updates := map[string]interface{}{
		"balance":    gorm.Expr("balance - ?", amount),
		"updated_at": common.GetTimestamp(),
	}
	for key, value := range fields {
		updates[key] = value
	}
	result := tx.Model(&GameCoinAccount{}).Where("user_id = ? AND balance >= ?", userId, amount).Updates(updates)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, ErrGameCoinInsufficient
	}
	return readGameCoinBalanceTx(tx, userId)
}

func readGameCoinBalanceTx(tx *gorm.DB, userId int) (int, error) {
	var balance int
	err := tx.Model(&GameCoinAccount{}).Where("user_id = ?", userId).Select("balance").Scan(&balance).Error
	return balance, err
}

// GameCoinQuota 返回兑换 coins 个游戏币应得的主额度：1 币 = 1 美元，即 QuotaPerUnit。
func GameCoinQuota(coins int) (int, error) {
	if coins < 1 || coins >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	if common.QuotaPerUnit <= 0 {
		return 0, errors.New("QuotaPerUnit must be positive")
	}
	quota, err := common.QuotaFromDecimalStrict(decimal.NewFromInt(int64(coins)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if err != nil {
		return 0, ErrQuotaOutOfRange
	}
	return quota, nil
}

// ConvertGameCoinsToQuota 把游戏币单向兑换为主额度。扣币、记流水、给 quota 入账在同一个
// 事务里，主钱包容量不够时整笔回滚，游戏币原样保留。
func ConvertGameCoinsToQuota(userId int, coins int) (quota int, balance int, err error) {
	quota, err = GameCoinQuota(coins)
	if err != nil {
		return 0, 0, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		balance, txErr = debitGameCoinsTx(tx, userId, coins, map[string]interface{}{
			"total_converted": gorm.Expr("total_converted + ?", coins),
		})
		if txErr != nil {
			return txErr
		}
		if txErr = tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeConvert,
			Amount:    -coins,
			Balance:   balance,
			Quota:     quota,
			CreatedAt: common.GetTimestamp(),
		}).Error; txErr != nil {
			return txErr
		}
		return creditTopUpQuota(tx, userId, quota, nil)
	})
	if err != nil {
		return 0, 0, err
	}
	syncCreditUserQuotaCache(userId, quota, "game coin convert")
	return quota, balance, nil
}

// AdjustGameCoins 是管理员给用户加减游戏币，delta 为正加币、为负扣币，扣币不会透支。
func AdjustGameCoins(userId int, delta int, remark string) (int, error) {
	if delta == 0 || delta <= -common.MaxQuota || delta >= common.MaxQuota {
		return 0, ErrGameCoinAmountInvalid
	}
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var users int64
		if err := tx.Model(&User{}).Where("id = ?", userId).Count(&users).Error; err != nil {
			return err
		}
		if users == 0 {
			return ErrGameCoinUserNotFound
		}
		var err error
		if delta > 0 {
			balance, err = creditGameCoinsTx(tx, userId, delta, map[string]interface{}{
				"total_earned": gorm.Expr("total_earned + ?", delta),
			})
		} else {
			balance, err = debitGameCoinsTx(tx, userId, -delta, nil)
		}
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeAdminAdjust,
			Amount:    delta,
			Balance:   balance,
			Remark:    remark,
			CreatedAt: common.GetTimestamp(),
		}).Error
	})
	return balance, err
}

// CreateGameCoinWithdrawal 在一个事务里扣币、占用当日全站额度并生成待发送的提现单。
// 它必须先于任何链上动作成功，之后每条失败路径都由 FailGameCoinWithdrawal 原路退回。
func CreateGameCoinWithdrawal(userId int, toAddress string, amount int, day string, dailyLimit int) (*GameCoinWithdrawal, int, error) {
	if amount < 1 || amount >= common.MaxQuota {
		return nil, 0, ErrGameCoinAmountInvalid
	}
	now := common.GetTimestamp()
	withdrawal := &GameCoinWithdrawal{
		TradeNo:   fmt.Sprintf("GW%d%s%s", userId, strconv.FormatInt(time.Now().UnixMilli(), 36), common.GetRandomString(6)),
		UserId:    userId,
		ToAddress: toAddress,
		Amount:    amount,
		Day:       day,
		Status:    GameCoinWithdrawalStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	var balance int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		balance, err = debitGameCoinsTx(tx, userId, amount, nil)
		if err != nil {
			return err
		}
		if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GameCoinWithdrawDay{Day: day}).Error; err != nil {
			return err
		}
		result := tx.Model(&GameCoinWithdrawDay{}).Where("day = ? AND used <= ?", day, dailyLimit-amount).
			Update("used", gorm.Expr("used + ?", amount))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrGameCoinDailyLimit
		}
		if err = tx.Create(withdrawal).Error; err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    userId,
			Type:      GameCoinLogTypeWithdraw,
			Amount:    -amount,
			Balance:   balance,
			RefId:     withdrawal.TradeNo,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, 0, err
	}
	return withdrawal, balance, nil
}

// GetGameCoinWithdrawDayUsed 返回某天全站已占用的提现额度。
func GetGameCoinWithdrawDayUsed(day string) (int, error) {
	var used int
	err := DB.Model(&GameCoinWithdrawDay{}).Where("day = ?", day).Select("used").Scan(&used).Error
	return used, err
}

// HasUnfinishedGameCoinWithdrawals 决定提现 worker 这一轮是否需要调度，空闲时不产生任务记录。
func HasUnfinishedGameCoinWithdrawals() bool {
	var id int
	err := DB.Model(&GameCoinWithdrawal{}).
		Where("status IN ?", unfinishedGameCoinWithdrawalStatuses).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

// CountGameCoinWithdrawalsByStatus 按状态统计提现单数量，供管理端概览。
func CountGameCoinWithdrawalsByStatus() (map[string]int64, error) {
	var rows []struct {
		Status string
		Total  int64
	}
	if err := DB.Model(&GameCoinWithdrawal{}).Select("status, COUNT(*) AS total").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Status] = row.Total
	}
	return counts, nil
}

// ListGameCoinWithdrawalsByStatus 按创建顺序返回某个状态的提现单。发送顺序决定 nonce 顺序，
// 所以必须按 id 升序处理。
func ListGameCoinWithdrawalsByStatus(status string, limit int) ([]*GameCoinWithdrawal, error) {
	var withdrawals []*GameCoinWithdrawal
	err := DB.Where("status = ?", status).Order("id asc").Limit(limit).Find(&withdrawals).Error
	return withdrawals, err
}

// MaxGameCoinWithdrawalNonce 返回某个热钱包在某条链上已经签出的最大 nonce。
// 签出过的 nonce 要么已上链、要么仍在内存池，新交易只能排在它们后面。
func MaxGameCoinWithdrawalNonce(fromAddress string, chainId int64) (int64, bool, error) {
	var result struct {
		MaxNonce *int64
	}
	err := DB.Model(&GameCoinWithdrawal{}).
		Select("MAX(nonce) AS max_nonce").
		Where("from_address = ? AND chain_id = ? AND tx_hash <> ?", fromAddress, chainId, "").
		Scan(&result).Error
	if err != nil || result.MaxNonce == nil {
		return 0, false, err
	}
	return *result.MaxNonce, true, nil
}

// MarkGameCoinWithdrawalSigned 在广播之前把签好的交易落库。只有这样，进程在广播前后退出时，
// 下一轮才能重发同一笔交易，而不是再签一笔新的、让用户收到两次。
func MarkGameCoinWithdrawalSigned(id int, chainId int64, tokenAddress string, fromAddress string, nonce int64, txHash string, rawTx string) (bool, error) {
	result := DB.Model(&GameCoinWithdrawal{}).
		Where("id = ? AND status = ?", id, GameCoinWithdrawalStatusPending).
		Updates(map[string]interface{}{
			"status":        GameCoinWithdrawalStatusSigned,
			"chain_id":      chainId,
			"token_address": tokenAddress,
			"from_address":  fromAddress,
			"nonce":         nonce,
			"tx_hash":       txHash,
			"raw_tx":        rawTx,
			"error":         "",
			"updated_at":    common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// MarkGameCoinWithdrawalSent 记录节点已接收这笔交易。
func MarkGameCoinWithdrawalSent(id int) (bool, error) {
	now := common.GetTimestamp()
	result := DB.Model(&GameCoinWithdrawal{}).
		Where("id = ? AND status = ?", id, GameCoinWithdrawalStatusSigned).
		Updates(map[string]interface{}{
			"status":       GameCoinWithdrawalStatusSent,
			"error":        "",
			"broadcast_at": now,
			"updated_at":   now,
		})
	return result.RowsAffected == 1, result.Error
}

// RecordGameCoinWithdrawalAttempt 记下一次没有结论的链上操作：状态不变，只更新最近错误与时间。
func RecordGameCoinWithdrawalAttempt(id int, broadcast bool, errText string) error {
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"error":      truncateRiskField(errText, 512),
		"updated_at": now,
	}
	if broadcast {
		updates["broadcast_at"] = now
	}
	return DB.Model(&GameCoinWithdrawal{}).Where("id = ?", id).Updates(updates).Error
}

// ConfirmGameCoinWithdrawal 把已上链的提现单标记为完成，并计入用户的累计提现。
func ConfirmGameCoinWithdrawal(id int) (bool, error) {
	confirmed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var withdrawal GameCoinWithdrawal
		if err := lockForUpdate(tx).Where("id = ?", id).First(&withdrawal).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		result := tx.Model(&GameCoinWithdrawal{}).
			Where("id = ? AND status = ?", id, GameCoinWithdrawalStatusSent).
			Updates(map[string]interface{}{
				"status":       GameCoinWithdrawalStatusConfirmed,
				"error":        "",
				"completed_at": now,
				"updated_at":   now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		confirmed = true
		return tx.Model(&GameCoinAccount{}).Where("user_id = ?", withdrawal.UserId).Updates(map[string]interface{}{
			"total_withdrawn": gorm.Expr("total_withdrawn + ?", withdrawal.Amount),
			"updated_at":      now,
		}).Error
	})
	return confirmed, err
}

// FailGameCoinWithdrawal 把尚未完成的提现单判定为失败：退回游戏币、释放当日额度、记退款流水。
// 状态用条件更新切换，同一笔提现最多退款一次。调用方必须已经确认这笔交易不会再上链。
func FailGameCoinWithdrawal(id int, reason string, detail string) (bool, error) {
	failed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var withdrawal GameCoinWithdrawal
		if err := lockForUpdate(tx).Where("id = ?", id).First(&withdrawal).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		result := tx.Model(&GameCoinWithdrawal{}).
			Where("id = ? AND status IN ?", id, unfinishedGameCoinWithdrawalStatuses).
			Updates(map[string]interface{}{
				"status":       GameCoinWithdrawalStatusFailed,
				"fail_reason":  truncateRiskField(reason, 255),
				"error":        truncateRiskField(detail, 512),
				"completed_at": now,
				"updated_at":   now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		failed = true
		if err := tx.Model(&GameCoinWithdrawDay{}).Where("day = ? AND used >= ?", withdrawal.Day, withdrawal.Amount).
			Update("used", gorm.Expr("used - ?", withdrawal.Amount)).Error; err != nil {
			return err
		}
		balance, err := creditGameCoinsTx(tx, withdrawal.UserId, withdrawal.Amount, nil)
		if err != nil {
			return err
		}
		return tx.Create(&GameCoinLog{
			UserId:    withdrawal.UserId,
			Type:      GameCoinLogTypeWithdrawRefund,
			Amount:    withdrawal.Amount,
			Balance:   balance,
			RefId:     withdrawal.TradeNo,
			Remark:    truncateRiskField(reason, 255),
			CreatedAt: now,
		}).Error
	})
	return failed, err
}

func GetUserGameCoinLogs(userId int, offset int, limit int) ([]*GameCoinLog, int64, error) {
	var logs []*GameCoinLog
	var total int64
	query := DB.Model(&GameCoinLog{}).Where("user_id = ?", userId)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&logs).Error
	return logs, total, err
}

func GetUserGameCoinWithdrawals(userId int, offset int, limit int) ([]*GameCoinWithdrawal, int64, error) {
	return queryGameCoinWithdrawals(DB.Model(&GameCoinWithdrawal{}).Where("user_id = ?", userId), offset, limit)
}

// GameCoinWithdrawalAdminView 在提现单之外带上用户名和原始错误，只用于管理端。
type GameCoinWithdrawalAdminView struct {
	GameCoinWithdrawal
	Username string `json:"username"`
	Error    string `json:"error"`
}

func GetAllGameCoinWithdrawals(status string, userId int, offset int, limit int) ([]*GameCoinWithdrawalAdminView, int64, error) {
	query := DB.Model(&GameCoinWithdrawal{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	withdrawals, total, err := queryGameCoinWithdrawals(query, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	userIds := make([]int, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		userIds = append(userIds, withdrawal.UserId)
	}
	usernames, err := usernamesByIds(userIds)
	if err != nil {
		return nil, 0, err
	}
	views := make([]*GameCoinWithdrawalAdminView, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		views = append(views, &GameCoinWithdrawalAdminView{
			GameCoinWithdrawal: *withdrawal,
			Username:           usernames[withdrawal.UserId],
			Error:              withdrawal.Error,
		})
	}
	return views, total, nil
}

func queryGameCoinWithdrawals(query *gorm.DB, offset int, limit int) ([]*GameCoinWithdrawal, int64, error) {
	var withdrawals []*GameCoinWithdrawal
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&withdrawals).Error
	return withdrawals, total, err
}

func usernamesByIds(userIds []int) (map[int]string, error) {
	usernames := make(map[int]string, len(userIds))
	if len(userIds) == 0 {
		return usernames, nil
	}
	var users []User
	if err := DB.Unscoped().Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		usernames[user.Id] = user.Username
	}
	return usernames, nil
}

// GameCoinAccountAdminView 在账户之外带上用户名，只用于管理端。
type GameCoinAccountAdminView struct {
	GameCoinAccount
	Username string `json:"username"`
}

// GetGameCoinAccounts 按余额从高到低列出账户，userId 大于 0 时只查这一个用户。
func GetGameCoinAccounts(userId int, offset int, limit int) ([]*GameCoinAccountAdminView, int64, error) {
	query := DB.Model(&GameCoinAccount{})
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var accounts []*GameCoinAccount
	if err := query.Order("balance desc").Order("user_id asc").Offset(offset).Limit(limit).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}
	userIds := make([]int, 0, len(accounts))
	for _, account := range accounts {
		userIds = append(userIds, account.UserId)
	}
	usernames, err := usernamesByIds(userIds)
	if err != nil {
		return nil, 0, err
	}
	views := make([]*GameCoinAccountAdminView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, &GameCoinAccountAdminView{GameCoinAccount: *account, Username: usernames[account.UserId]})
	}
	return views, total, nil
}
