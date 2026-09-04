package model

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

// 风控排行榜/下钻查询。全部实时聚合 logs 表(走 LOG_DB),不落新表。
// 三库兼容要点:
//   - count(distinct x)、min/max 三库均支持;
//   - HAVING / ORDER BY 一律写完整聚合表达式,不引用 SELECT 别名
//     (PostgreSQL 的 HAVING 不支持别名);
//   - ip / ua / user_id / username 均非保留字,无需 quote。

const (
	riskMaxWindowHours     = 24 * 7 // 统计窗口上限 7 天,防止全表扫描
	riskDefaultWindowHours = 24
	riskDefaultLimit       = 50
	riskMaxLimit           = 500
)

// 风控排行榜维度标识,供 controller 路由分发使用。
// user_overview / ip_overview 是把多个单指标榜合并成一行的总览视图;
// 其余单指标标识保留,自动封禁扫描与旧前端仍在使用。
const (
	RiskMetricIpMultiUser     = "ip_multi_user"
	RiskMetricUserMultiIp     = "user_multi_ip"
	RiskMetricUa              = "ua"
	RiskMetricIpMultiToken    = "ip_multi_token"
	RiskMetricUserTinyRequest = "user_tiny_request"
	RiskMetricUserErrorBurst  = "user_error_burst"
	RiskMetricTokenMultiIp    = "token_multi_ip"
	RiskMetricUserOverview    = "user_overview"
	RiskMetricIpOverview      = "ip_overview"
)

// IpRankItem 单 IP 关联多用户排行项。
type IpRankItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	UserCount    int    `json:"user_count" gorm:"column:user_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// UserRankItem 单用户使用多 IP 排行项。
type UserRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	IpCount      int    `json:"ip_count" gorm:"column:ip_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// IpTokenRankItem 单 IP 使用多令牌排行项(批量测活的典型特征)。
type IpTokenRankItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	TokenCount   int    `json:"token_count" gorm:"column:token_count"`
	UserCount    int    `json:"user_count" gorm:"column:user_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// UserTinyRequestRankItem 用户微量请求排行项(自动测活的典型特征)。
type UserTinyRequestRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	TokenCount   int    `json:"token_count" gorm:"column:token_count"`
}

// UserErrorRankItem 用户错误请求排行项。
type UserErrorRankItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// TokenIpRankItem 单令牌被多 IP 使用排行项(密钥泄露/倒卖的典型特征)。
type TokenIpRankItem struct {
	TokenId      int    `json:"token_id" gorm:"column:token_id"`
	TokenName    string `json:"token_name" gorm:"column:token_name"`
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	IpCount      int    `json:"ip_count" gorm:"column:ip_count"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
}

// RiskUserDetailItem 某 IP 下钻出的关联用户明细。
type RiskUserDetailItem struct {
	UserId       int    `json:"user_id" gorm:"column:user_id"`
	Username     string `json:"username" gorm:"column:username"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskIpDetailItem 某用户下钻出的 IP 明细。
type RiskIpDetailItem struct {
	Ip           string `json:"ip" gorm:"column:ip"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// normalizeRiskWindow 归一化统计窗口(小时)。
func normalizeRiskWindow(hours int) int64 {
	if hours <= 0 {
		hours = riskDefaultWindowHours
	}
	if hours > riskMaxWindowHours {
		hours = riskMaxWindowHours
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}

func normalizeRiskLimit(limit int) int {
	if limit <= 0 {
		limit = riskDefaultLimit
	}
	if limit > riskMaxLimit {
		limit = riskMaxLimit
	}
	return limit
}

// riskLogTypes 参与风控统计的日志类型(消费 + 错误)。
func riskLogTypes() []int {
	return []int{LogTypeConsume, LogTypeError}
}

// 多账号关联(一人多号)统计的口径与自动封禁扫描不同:认定同一人持有多个账号,
// 靠的是这些账号在同一来源地址上留下的任何痕迹——注册、登录、签到、安全操作、
// 调用都算证据,而不只是调用。
//
// 有意排除充值日志(LogTypeTopup):它的来源是支付网关回调的地址,凡是充过值的
// 用户都会挂在同一个网关 IP 上,纳入统计会把整站付费用户关联成一伙。
// 同样排除 LogTypeUnknown:没有明确语义的行不构成证据。
const (
	// 账号事件口径每个账号只留寥寥数行,可以回溯很久;一旦并入调用日志,
	// 行数会涨几个数量级,窗口必须收回到与其他榜单相同的量级。
	multiAccountMaxWindowHours        = 24 * 90
	multiAccountRequestMaxWindowHours = 24 * 7
	multiAccountDefaultWindowHours    = 24 * 7
	multiAccountDefaultMinUsers       = 2
	multiAccountMaxMinUsers           = 1000
)

// MultiAccountQuery 是多账号关联统计的查询参数。
type MultiAccountQuery struct {
	Hours           int
	Limit           int
	MinUsers        int   // 关联账号数下限,低于该值的地址不进榜
	IncludeRequests bool  // 是否把调用与错误日志也算作证据
	ExcludeUserIds  []int // 全局白名单账号
}

// MultiAccountIpItem 单个来源地址上的多账号关联项。
type MultiAccountIpItem struct {
	Ip            string `json:"ip" gorm:"column:ip"`
	UserCount     int    `json:"user_count" gorm:"column:user_count"`
	RegisterCount int    `json:"register_count" gorm:"column:register_count"`
	LoginCount    int    `json:"login_count" gorm:"column:login_count"`
	EventCount    int    `json:"event_count" gorm:"column:event_count"`
	FirstSeen     int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen      int64  `json:"last_seen" gorm:"column:last_seen"`
}

// MultiAccountUserItem 某地址下关联到的单个账号明细。
type MultiAccountUserItem struct {
	UserId        int    `json:"user_id" gorm:"column:user_id"`
	Username      string `json:"username" gorm:"column:username"`
	RegisterCount int    `json:"register_count" gorm:"column:register_count"`
	LoginCount    int    `json:"login_count" gorm:"column:login_count"`
	RequestCount  int    `json:"request_count" gorm:"column:request_count"`
	EventCount    int    `json:"event_count" gorm:"column:event_count"`
	FirstSeen     int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen      int64  `json:"last_seen" gorm:"column:last_seen"`
	// 以下字段不在日志库里,由 AttachMultiAccountUserProfiles 补齐
	Status      int                   `json:"status" gorm:"-"`
	DisplayName string                `json:"display_name" gorm:"-"`
	Bindings    []MultiAccountBinding `json:"bindings" gorm:"-"`
	// RegisterMethod 是注册渠道(password / github / wechat / 自定义 OAuth 的 slug 等),
	// 取自注册日志;日志被清理或早于该字段上线时为空。
	RegisterMethod string `json:"register_method,omitempty" gorm:"-"`
	// Role 只用于服务端的越权判定,不返回给前端;能否操作该账号由 CanManage 表达。
	Role      int  `json:"-" gorm:"-"`
	CanManage bool `json:"can_manage" gorm:"-"`
}

// MultiAccountBinding 是账号的一条第三方身份绑定。
// 同一人的多个小号往往共用一个身份来源(同一邮箱域名、同一 OAuth 提供商),
// 或干脆只有密码没有任何绑定,这些都是比"同出口地址"更硬的研判证据。
type MultiAccountBinding struct {
	Type string `json:"type"` // email | github | discord | oidc | wechat | telegram | linuxdo | steam | custom
	// Name 只在自定义 OAuth 时有值:提供商名称由管理员配置,前端无从映射。
	// 内置类型的展示名由前端按 Type 本地化。
	Name       string `json:"name,omitempty"`
	Identifier string `json:"identifier"`
	// IsRegistration 标记账号是用这条绑定注册的。绑定多了以后,注册来源才是
	// 追同一人的那条线——其余绑定可能是事后补绑的。
	IsRegistration bool `json:"is_registration,omitempty"`
}

// EffectiveLimits 返回本次查询实际生效的边界，供前端回显：用户填的值可能被钳制，
// 页面上显示的必须是真正用于查询的那个。
func (q MultiAccountQuery) EffectiveLimits() (minUsers int, maxWindowHours int) {
	maxWindowHours = multiAccountMaxWindowHours
	if q.IncludeRequests {
		maxWindowHours = multiAccountRequestMaxWindowHours
	}
	return normalizeMultiAccountMinUsers(q.MinUsers), maxWindowHours
}

func multiAccountLogTypes(includeRequests bool) []int {
	types := []int{LogTypeRegister, LogTypeLogin, LogTypeSystem, LogTypeManage}
	if includeRequests {
		types = append(types, LogTypeConsume, LogTypeError, LogTypeRefund)
	}
	return types
}

// normalizeMultiAccountWindow 归一化窗口起点,上限随是否包含调用日志而变。
func normalizeMultiAccountWindow(hours int, includeRequests bool) int64 {
	max := multiAccountMaxWindowHours
	if includeRequests {
		max = multiAccountRequestMaxWindowHours
	}
	if hours <= 0 {
		hours = multiAccountDefaultWindowHours
	}
	if hours > max {
		hours = max
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}

func normalizeMultiAccountMinUsers(minUsers int) int {
	if minUsers < multiAccountDefaultMinUsers {
		return multiAccountDefaultMinUsers
	}
	if minUsers > multiAccountMaxMinUsers {
		return multiAccountMaxMinUsers
	}
	return minUsers
}

// riskTypeCountExpr 统计某类日志的行数。logType 是包内常量,不会引入注入面。
func riskTypeCountExpr(logType int) string {
	return fmt.Sprintf("count(case when type = %d then 1 end)", logType)
}

// multiAccountBaseQuery 是排行与下钻共用的过滤条件。
func multiAccountBaseQuery(q MultiAccountQuery) *gorm.DB {
	tx := LOG_DB.Table("logs").
		Where("created_at >= ?", normalizeMultiAccountWindow(q.Hours, q.IncludeRequests)).
		Where("type IN ?", multiAccountLogTypes(q.IncludeRequests)).
		Where("ip <> ''").
		Where("user_id > 0")
	return riskExcludeUsers(tx, q.ExcludeUserIds)
}

// GetMultiAccountRanking 返回关联账号数达到阈值的来源地址,按关联账号数排行。
// 这是纯统计接口:只负责把可疑地址摆出来供人工研判,不触发任何自动处置。
func GetMultiAccountRanking(q MultiAccountQuery) ([]MultiAccountIpItem, error) {
	var items []MultiAccountIpItem
	err := multiAccountBaseQuery(q).
		Select(strings.Join([]string{
			"ip",
			"count(distinct user_id) as user_count",
			riskTypeCountExpr(LogTypeRegister) + " as register_count",
			riskTypeCountExpr(LogTypeLogin) + " as login_count",
			"count(*) as event_count",
			"min(created_at) as first_seen",
			"max(created_at) as last_seen",
		}, ", ")).
		Group("ip").
		Having("count(distinct user_id) >= ?", normalizeMultiAccountMinUsers(q.MinUsers)).
		Order("user_count desc, register_count desc, event_count desc").
		Limit(normalizeRiskLimit(q.Limit)).
		Find(&items).Error
	return items, err
}

// GetMultiAccountUsers 下钻某个地址,返回它关联的全部账号及各自的证据构成。
func GetMultiAccountUsers(ip string, q MultiAccountQuery) ([]MultiAccountUserItem, error) {
	if ip == "" {
		return nil, errors.New("ip is required")
	}
	var items []MultiAccountUserItem
	err := multiAccountBaseQuery(q).
		Where("ip = ?", ip).
		Select(strings.Join([]string{
			"user_id",
			"max(username) as username",
			riskTypeCountExpr(LogTypeRegister) + " as register_count",
			riskTypeCountExpr(LogTypeLogin) + " as login_count",
			riskTypeCountExpr(LogTypeConsume) + " as request_count",
			"count(*) as event_count",
			"min(created_at) as first_seen",
			"max(created_at) as last_seen",
		}, ", ")).
		Group("user_id").
		Order("register_count desc, event_count desc").
		Limit(normalizeRiskLimit(q.Limit)).
		Find(&items).Error
	return items, err
}

// AttachMultiAccountUserProfiles 从主库补齐账号的当前状态、昵称、角色与第三方绑定。
// 日志库可以独立部署，不能与 users 表 join，因此分两步查；补齐失败只记日志，
// 统计结果照常返回——少一列展示信息不该让整个研判页面打不开。
// 本函数只取数,不做越权判定:绑定明细是否返回给调用者由 controller 按角色决定。
func AttachMultiAccountUserProfiles(items []MultiAccountUserItem) {
	if len(items) == 0 {
		return
	}
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserId)
	}
	var users []User
	if err := DB.Model(&User{}).
		Select("id", "status", "role", "display_name", "email", "github_id", "discord_id",
			"oidc_id", "wechat_id", "telegram_id", "linux_do_id", "steam_openid").
		Where("id IN ?", ids).Find(&users).Error; err != nil {
		common.SysLog("risk control: failed to load user status for multi-account detail: " + err.Error())
		return
	}
	byId := make(map[int]User, len(users))
	for _, user := range users {
		byId[user.Id] = user
	}
	customBindings := customOAuthBindingsByUser(ids)
	registerMethods := registerMethodsByUser(ids)
	for i := range items {
		user, ok := byId[items[i].UserId]
		if !ok {
			continue
		}
		items[i].Status = user.Status
		items[i].Role = user.Role
		items[i].DisplayName = user.DisplayName
		items[i].RegisterMethod = registerMethods[user.Id]
		bindings := append(builtInAccountBindings(user), customBindings[user.Id]...)
		markRegistrationBinding(bindings, items[i].RegisterMethod)
		items[i].Bindings = bindings
	}
}

// markRegistrationBinding 用注册日志里的渠道标出账号的注册来源绑定。
// 自定义 OAuth 的注册标记来自绑定表本身,更权威,已有标记时不再覆盖;
// 内置身份没有这样的字段,只能靠注册日志回填。
// 密码注册不对应任何绑定,注册渠道单独随账号返回。
func markRegistrationBinding(bindings []MultiAccountBinding, registerMethod string) {
	if registerMethod == "" {
		return
	}
	for _, binding := range bindings {
		if binding.IsRegistration {
			return
		}
	}
	for i := range bindings {
		if bindings[i].Type == registerMethod {
			bindings[i].IsRegistration = true
			return
		}
	}
}

// registerMethodsByUser 从注册日志取这些账号的注册渠道。
// 渠道写在 logs.other 的 JSON 里,三库的 JSON 函数互不兼容,因此取回后在应用层解析
// (与错误状态码分布的处理一致)。一个账号只会留下一条注册日志,取最早的那条。
func registerMethodsByUser(userIds []int) map[int]string {
	var rows []struct {
		UserId int    `gorm:"column:user_id"`
		Other  string `gorm:"column:other"`
	}
	if err := LOG_DB.Table("logs").
		Select("user_id", "other").
		Where("type = ?", LogTypeRegister).
		Where("user_id IN ?", userIds).
		Order("created_at asc, id asc").
		Find(&rows).Error; err != nil {
		common.SysLog("risk control: failed to load register methods for multi-account detail: " + err.Error())
		return nil
	}

	methods := make(map[int]string, len(rows))
	for _, row := range rows {
		if _, ok := methods[row.UserId]; ok || row.Other == "" {
			continue
		}
		var parsed struct {
			RegisterMethod string `json:"register_method"`
		}
		if common.UnmarshalJsonStr(row.Other, &parsed) != nil {
			continue
		}
		if parsed.RegisterMethod != "" {
			methods[row.UserId] = parsed.RegisterMethod
		}
	}
	return methods
}

// builtInAccountBindings 返回账号已填写的内置身份绑定,顺序与用户管理的绑定弹窗一致。
func builtInAccountBindings(user User) []MultiAccountBinding {
	candidates := []MultiAccountBinding{
		{Type: "email", Identifier: user.Email},
		{Type: "github", Identifier: user.GitHubId},
		{Type: "discord", Identifier: user.DiscordId},
		{Type: "oidc", Identifier: user.OidcId},
		{Type: "wechat", Identifier: user.WeChatId},
		{Type: "telegram", Identifier: user.TelegramId},
		{Type: "linuxdo", Identifier: user.LinuxDOId},
		{Type: "steam", Identifier: user.SteamOpenId},
	}
	bindings := make([]MultiAccountBinding, 0, len(candidates))
	for _, binding := range candidates {
		binding.Identifier = strings.TrimSpace(binding.Identifier)
		if binding.Identifier != "" {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

// customOAuthBindingsByUser 批量取这些账号的自定义 OAuth 绑定,按账号分组。
// 提供商名称查不到时回退为编号:一条绑定的存在本身就是证据,不该因为提供商被删掉就消失。
func customOAuthBindingsByUser(userIds []int) map[int][]MultiAccountBinding {
	var bindings []UserOAuthBinding
	if err := ReadDB().Where("user_id IN ?", userIds).Order("provider_id asc").
		Find(&bindings).Error; err != nil {
		common.SysLog("risk control: failed to load custom oauth bindings for multi-account detail: " + err.Error())
		return nil
	}
	if len(bindings) == 0 {
		return nil
	}

	providerIds := make([]int, 0, len(bindings))
	seen := make(map[int]struct{}, len(bindings))
	for _, binding := range bindings {
		if _, ok := seen[binding.ProviderId]; ok {
			continue
		}
		seen[binding.ProviderId] = struct{}{}
		providerIds = append(providerIds, binding.ProviderId)
	}
	var providers []CustomOAuthProvider
	if err := ReadDB().Model(&CustomOAuthProvider{}).Select("id", "name").
		Where("id IN ?", providerIds).Find(&providers).Error; err != nil {
		common.SysLog("risk control: failed to load custom oauth providers for multi-account detail: " + err.Error())
	}
	names := make(map[int]string, len(providers))
	for _, provider := range providers {
		names[provider.Id] = provider.Name
	}

	grouped := make(map[int][]MultiAccountBinding, len(userIds))
	for _, binding := range bindings {
		name := names[binding.ProviderId]
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("OAuth #%d", binding.ProviderId)
		}
		grouped[binding.UserId] = append(grouped[binding.UserId], MultiAccountBinding{
			Type:           "custom",
			Name:           name,
			Identifier:     binding.ProviderUserId,
			IsRegistration: binding.IsRegistration,
		})
	}
	return grouped
}

// riskExcludeUsers 把全局白名单账号产生的日志行从风控统计中剔除。
// 这些账号的流量既不该让自己上榜被处置,也不该把自己使用的 IP 顶上 IP 维度排行,
// 否则运营者自己的出口地址会被自动封禁,连带拦下同出口的其他正常用户。
// 空切片必须短路:GORM 会把空切片渲染成 NOT IN (NULL),那会过滤掉全部行。
func riskExcludeUsers(tx *gorm.DB, userIds []int) *gorm.DB {
	if len(userIds) == 0 {
		return tx
	}
	return tx.Where("user_id NOT IN ?", userIds)
}

// GetIpMultiUserRanking 返回窗口内的全部 IP，按关联用户数排行。
func GetIpMultiUserRanking(hours int, limit int, excludeUserIds []int) ([]IpRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []IpRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("ip").
		Order("user_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserMultiIpRanking 返回窗口内使用 IP 数最多的用户(仅含使用 >1 IP 的用户)。
func GetUserMultiIpRanking(hours int, limit int, excludeUserIds []int) ([]UserRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(distinct ip) as ip_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []UserRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Having("count(distinct ip) > ?", 1).
		Order("ip_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetIpMultiTokenRanking 返回窗口内使用令牌数最多的 IP(批量测活检测)。
// 只统计带令牌的请求(token_id > 0),消费与错误日志均计入,
// 保证测活失败(密钥无效)的请求同样被观测到。
func GetIpMultiTokenRanking(hours int, limit int, excludeUserIds []int) ([]IpTokenRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("ip, count(distinct token_id) as token_count, count(distinct user_id) as user_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("token_id > 0")

	var items []IpTokenRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("ip").
		Order("token_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserTinyRequestRanking 返回窗口内微量请求数最多的用户(自动测活检测)。
// 微量请求指 prompt 与 completion tokens 均不超过 maxTokens 的成功消费请求;
// 该统计不依赖 IP/UA 日志开关,token 数始终入库。
func GetUserTinyRequestRanking(hours int, limit int, maxTokens int, excludeUserIds []int) ([]UserTinyRequestRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(*) as request_count, count(distinct token_id) as token_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type = ?", LogTypeConsume).
		Where("user_id > 0").
		Where("prompt_tokens <= ?", maxTokens).
		Where("completion_tokens <= ?", maxTokens)

	var items []UserTinyRequestRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Order("request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetUserErrorRanking 返回窗口内错误请求数最多的用户。
func GetUserErrorRanking(hours int, limit int, excludeUserIds []int) ([]UserErrorRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("user_id, max(username) as username, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type = ?", LogTypeError).
		Where("user_id > 0")

	var items []UserErrorRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("user_id").
		Order("request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// GetTokenMultiIpRanking 返回窗口内被最多不同 IP 使用的令牌(仅含 >1 IP 的令牌)。
// 单令牌短期被大量 IP 使用是密钥泄露或倒卖的典型特征。
func GetTokenMultiIpRanking(hours int, limit int, excludeUserIds []int) ([]TokenIpRankItem, error) {
	tx := LOG_DB.Table("logs").
		Select("token_id, max(token_name) as token_name, max(user_id) as user_id, max(username) as username, count(distinct ip) as ip_count, count(*) as request_count").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("token_id > 0")

	var items []TokenIpRankItem
	err := riskExcludeUsers(tx, excludeUserIds).
		Group("token_id").
		Having("count(distinct ip) > ?", 1).
		Order("ip_count desc, request_count desc").
		Limit(normalizeRiskLimit(limit)).
		Find(&items).Error
	return items, err
}

// 合并统计项:原先分散在多个排行榜的指标聚合到一行,便于交叉判读
// (例如 IP 数高而令牌数低,说明单个令牌被多 IP 使用,疑似密钥泄露)。
// 三个维度共用同一组数值口径,具体的 UA、错误状态码分布走下钻详情。

// UserOverviewItem 是用户维度的合并统计项。
type UserOverviewItem struct {
	UserId           int    `json:"user_id" gorm:"column:user_id"`
	Username         string `json:"username" gorm:"column:username"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	IpCount          int    `json:"ip_count" gorm:"column:ip_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// IpOverviewItem 是 IP 维度的合并统计项。
type IpOverviewItem struct {
	Ip               string `json:"ip" gorm:"column:ip"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	UserCount        int    `json:"user_count" gorm:"column:user_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// UaOverviewItem 是 UA 维度的合并统计项:同一客户端标识被多少人、多少 IP 使用。
type UaOverviewItem struct {
	Ua               string `json:"ua" gorm:"column:ua"`
	RequestCount     int    `json:"request_count" gorm:"column:request_count"`
	UserCount        int    `json:"user_count" gorm:"column:user_count"`
	IpCount          int    `json:"ip_count" gorm:"column:ip_count"`
	TokenCount       int    `json:"token_count" gorm:"column:token_count"`
	TinyRequestCount int    `json:"tiny_request_count" gorm:"column:tiny_request_count"`
	ErrorCount       int    `json:"error_count" gorm:"column:error_count"`
	FirstSeen        int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen         int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskOverviewQuery 是合并排行的查询参数。
type RiskOverviewQuery struct {
	Hours          int
	Limit          int
	TinyMaxTokens  int    // 微量请求判定阈值(输入与输出 tokens 均不超过该值)
	SortBy         string // 排序字段,不在白名单内时回退请求数
	SortOrder      string // asc | desc,默认 desc
	ExcludeUserIds []int  // 排除这些用户产生的日志行(风控白名单)
}

// riskOverviewDefaultSort 是合并排行的默认排序字段。
const riskOverviewDefaultSort = "request_count"

// 条件计数一律用 count(case when ... then 1 end):CASE 在条件不成立时返回 NULL 而被
// count 忽略,三库语义一致且返回类型确定为整数(sum 在 MySQL 下返回 DECIMAL)。
const (
	riskDistinctIpExpr    = "count(distinct case when ip <> '' then ip end)"
	riskDistinctUserExpr  = "count(distinct case when user_id > 0 then user_id end)"
	riskDistinctTokenExpr = "count(distinct case when token_id > 0 then token_id end)"
)

// riskErrorCountExpr 统计窗口内的错误请求数。
var riskErrorCountExpr = fmt.Sprintf("count(case when type = %d then 1 end)", LogTypeError)

// riskTinyRequestCountExpr 返回微量请求的条件计数表达式。
// maxTokens 先夹到合法区间再内联,保证进入 SQL 的始终是小整数。
func riskTinyRequestCountExpr(maxTokens int) string {
	if maxTokens < 1 {
		maxTokens = operation_setting.RiskDefaultTinyRequestMaxTokens
	} else if maxTokens > operation_setting.RiskMaxTinyRequestMaxTokens {
		maxTokens = operation_setting.RiskMaxTinyRequestMaxTokens
	}
	return fmt.Sprintf("count(case when type = %d and prompt_tokens <= %d and completion_tokens <= %d then 1 end)",
		LogTypeConsume, maxTokens, maxTokens)
}

// riskOverviewSharedSelects 返回三个维度共有的聚合列(不含分组列与维度专属的去重计数)。
func riskOverviewSharedSelects(tinyExpr string) []string {
	return []string{
		"count(*) as request_count",
		riskDistinctTokenExpr + " as token_count",
		tinyExpr + " as tiny_request_count",
		riskErrorCountExpr + " as error_count",
		"min(created_at) as first_seen",
		"max(created_at) as last_seen",
	}
}

// riskOverviewSharedSortExprs 返回三个维度共有的可排序字段映射。
// 调用方再补上自己维度专属的字段(ip_count / user_count)。
func riskOverviewSharedSortExprs(tinyExpr string) map[string]string {
	return map[string]string{
		"request_count":      "count(*)",
		"token_count":        riskDistinctTokenExpr,
		"tiny_request_count": tinyExpr,
		"error_count":        riskErrorCountExpr,
		"first_seen":         "min(created_at)",
		"last_seen":          "max(created_at)",
	}
}

// riskOverviewOrderBy 按白名单映射解析排序字段并拼出完整的 ORDER BY 表达式。
// sortBy 不在映射内时回退请求数,因此外部传入的任意字符串都不会进入 SQL;
// 表达式而非 SELECT 别名与本文件其余查询保持一致。tieBreaker 让同值行的顺序稳定。
func riskOverviewOrderBy(exprs map[string]string, sortBy string, sortOrder string, tieBreaker string) string {
	expr, ok := exprs[sortBy]
	if !ok {
		expr = exprs[riskOverviewDefaultSort]
	}
	direction := "desc"
	if strings.EqualFold(sortOrder, "asc") {
		direction = "asc"
	}
	return expr + " " + direction + ", " + tieBreaker + " asc"
}

// GetUserOverviewRanking 返回窗口内的用户合并排行。
// 一条 SQL 聚合出全部指标,保证按任一字段取 top N 时同行的其余指标都是真实值。
func GetUserOverviewRanking(query RiskOverviewQuery) ([]UserOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["ip_count"] = riskDistinctIpExpr

	selects := append([]string{
		"user_id",
		"max(username) as username",
		riskDistinctIpExpr + " as ip_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("user_id > 0")

	var items []UserOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("user_id").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "user_id")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// GetIpOverviewRanking 返回窗口内的 IP 合并排行。
// ExcludeUserIds 排除白名单用户产生的日志行:纯由白名单用户使用的 IP 因此完全不上榜,
// 混合使用的 IP 只统计非白名单部分。
func GetIpOverviewRanking(query RiskOverviewQuery) ([]IpOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["user_count"] = riskDistinctUserExpr

	selects := append([]string{
		"ip",
		riskDistinctUserExpr + " as user_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''")

	var items []IpOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("ip").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "ip")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// GetUaOverviewRanking 返回窗口内的 UA 合并排行:同一客户端标识被多少用户、多少 IP 使用。
// 同一 UA 覆盖大量用户与 IP 通常是脚本或代理工具,也是配置 UA 黑名单的依据。
func GetUaOverviewRanking(query RiskOverviewQuery) ([]UaOverviewItem, error) {
	tinyExpr := riskTinyRequestCountExpr(query.TinyMaxTokens)
	sortExprs := riskOverviewSharedSortExprs(tinyExpr)
	sortExprs["user_count"] = riskDistinctUserExpr
	sortExprs["ip_count"] = riskDistinctIpExpr

	selects := append([]string{
		"ua",
		riskDistinctUserExpr + " as user_count",
		riskDistinctIpExpr + " as ip_count",
	}, riskOverviewSharedSelects(tinyExpr)...)

	tx := LOG_DB.Table("logs").
		Select(strings.Join(selects, ", ")).
		Where("created_at >= ?", normalizeRiskWindow(query.Hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ua <> ''")

	var items []UaOverviewItem
	err := riskExcludeUsers(tx, query.ExcludeUserIds).
		Group("ua").
		Order(riskOverviewOrderBy(sortExprs, query.SortBy, query.SortOrder, "ua")).
		Limit(normalizeRiskLimit(query.Limit)).
		Find(&items).Error
	return items, err
}

// 下钻详情:围绕单个目标(用户 / IP / UA)展开它的关联对象与错误分布。
// 排行榜只放数值,具体的 UA 文本、错误状态码这类明细都在这里查。

// 下钻目标类型。
const (
	RiskDetailTypeUser = "user"
	RiskDetailTypeIp   = "ip"
	RiskDetailTypeUa   = "ua"
)

// RiskUaDetailItem 某目标使用的 UA 明细。
type RiskUaDetailItem struct {
	Ua           string `json:"ua" gorm:"column:ua"`
	RequestCount int    `json:"request_count" gorm:"column:request_count"`
	FirstSeen    int64  `json:"first_seen" gorm:"column:first_seen"`
	LastSeen     int64  `json:"last_seen" gorm:"column:last_seen"`
}

// RiskErrorStatusItem 某目标的错误状态码分布项。
type RiskErrorStatusItem struct {
	StatusCode int    `json:"status_code"`
	ErrorCode  string `json:"error_code"`
	Count      int    `json:"count"`
}

// RiskDetailTarget 指定下钻的目标。
type RiskDetailTarget struct {
	Type           string // user | ip | ua
	Value          string // 用户 ID / IP / UA 原文
	Hours          int
	ExcludeUserIds []int // 与排行榜口径一致地排除白名单用户
}

// riskDetailQuery 构造限定到单个下钻目标的基础查询。
func riskDetailQuery(target RiskDetailTarget) (*gorm.DB, error) {
	tx := LOG_DB.Table("logs").
		Where("created_at >= ?", normalizeRiskWindow(target.Hours)).
		Where("type IN ?", riskLogTypes())

	switch target.Type {
	case RiskDetailTypeUser:
		userId, err := strconv.Atoi(strings.TrimSpace(target.Value))
		if err != nil || userId <= 0 {
			return nil, errors.New("无效的用户 ID")
		}
		tx = tx.Where("user_id = ?", userId)
	case RiskDetailTypeIp:
		if strings.TrimSpace(target.Value) == "" {
			return nil, errors.New("IP 不能为空")
		}
		tx = tx.Where("ip = ?", target.Value)
	case RiskDetailTypeUa:
		if target.Value == "" {
			return nil, errors.New("UA 不能为空")
		}
		tx = tx.Where("ua = ?", target.Value)
	default:
		return nil, errors.New("无效的下钻类型")
	}

	return riskExcludeUsers(tx, target.ExcludeUserIds), nil
}

// GetRiskDetailUsers 下钻:目标关联的用户明细。
func GetRiskDetailUsers(target RiskDetailTarget) ([]RiskUserDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskUserDetailItem
	err = tx.Select("user_id, max(username) as username, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("user_id > 0").
		Group("user_id").
		Order("count(*) desc, user_id asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetRiskDetailIps 下钻:目标使用的 IP 明细。
func GetRiskDetailIps(target RiskDetailTarget) ([]RiskIpDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskIpDetailItem
	err = tx.Select("ip, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("ip <> ''").
		Group("ip").
		Order("count(*) desc, ip asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// GetRiskDetailUas 下钻:目标使用的 UA 明细。
func GetRiskDetailUas(target RiskDetailTarget) ([]RiskUaDetailItem, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, err
	}

	var items []RiskUaDetailItem
	err = tx.Select("ua, count(*) as request_count, min(created_at) as first_seen, max(created_at) as last_seen").
		Where("ua <> ''").
		Group("ua").
		Order("count(*) desc, ua asc").
		Limit(riskMaxLimit).
		Find(&items).Error
	return items, err
}

// riskErrorSampleLimit 错误状态码分布的采样条数上限。
const riskErrorSampleLimit = 2000

// GetRiskDetailErrorStatuses 下钻:目标的错误状态码分布,按出现次数降序。
// status_code 与 error_code 存放在 logs.other 的 JSON 里,而三库的 JSON 函数互不兼容,
// 因此取最近若干条错误日志在应用层解析聚合。第二个返回值表示样本已达上限,
// 即分布基于最近的样本而非窗口内全量错误。
func GetRiskDetailErrorStatuses(target RiskDetailTarget) ([]RiskErrorStatusItem, bool, error) {
	tx, err := riskDetailQuery(target)
	if err != nil {
		return nil, false, err
	}

	var payloads []string
	if err := tx.Where("type = ?", LogTypeError).
		Order("created_at desc, id desc").
		Limit(riskErrorSampleLimit).
		Pluck("other", &payloads).Error; err != nil {
		return nil, false, err
	}

	grouped := map[string]*RiskErrorStatusItem{}
	for _, raw := range payloads {
		if raw == "" {
			continue
		}
		var parsed struct {
			StatusCode int    `json:"status_code"`
			ErrorCode  string `json:"error_code"`
		}
		if common.UnmarshalJsonStr(raw, &parsed) != nil {
			continue
		}
		if parsed.StatusCode == 0 && parsed.ErrorCode == "" {
			continue
		}
		key := strconv.Itoa(parsed.StatusCode) + "\x00" + parsed.ErrorCode
		if item, ok := grouped[key]; ok {
			item.Count++
			continue
		}
		grouped[key] = &RiskErrorStatusItem{
			StatusCode: parsed.StatusCode,
			ErrorCode:  parsed.ErrorCode,
			Count:      1,
		}
	}

	items := make([]RiskErrorStatusItem, 0, len(grouped))
	for _, item := range grouped {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		if items[i].StatusCode != items[j].StatusCode {
			return items[i].StatusCode < items[j].StatusCode
		}
		return items[i].ErrorCode < items[j].ErrorCode
	})
	return items, len(payloads) >= riskErrorSampleLimit, nil
}

// riskWhitelistIpSampleLimit 拉取白名单账号近期地址的条数上限。
// 白名单是运营者自用的少数账号,正常情况远达不到上限;触顶说明白名单里放了
// 高流量账号,此时豁免判定可能不完整,调用方会记录日志。
const riskWhitelistIpSampleLimit = 1000

// GetRecentIpsByUsers 返回窗口内这些用户使用过的去重 IP。
// 自动封禁用它判断封禁目标(单地址或 IPv6 归并后的前缀)是否覆盖全局白名单账号。
// 之所以取回地址在应用层比对而不是用 SQL 判定:封禁目标可能是 CIDR,
// 而三库都没有可移植的网段包含运算。第二个返回值表示已达采样上限。
func GetRecentIpsByUsers(hours int, userIds []int) ([]string, bool, error) {
	if len(userIds) == 0 {
		return nil, false, nil
	}

	var ips []string
	err := LOG_DB.Table("logs").
		Where("created_at >= ?", normalizeRiskWindow(hours)).
		Where("type IN ?", riskLogTypes()).
		Where("ip <> ''").
		Where("user_id IN ?", userIds).
		Distinct().
		Limit(riskWhitelistIpSampleLimit).
		Pluck("ip", &ips).Error
	return ips, len(ips) >= riskWhitelistIpSampleLimit, err
}

// HasIpLogsSince 判断某 IP 在 since 之后是否还有风控统计口径内的日志行,
// excludeUserIds 与排行统计一样剔除全局白名单账号的行。
// 扫描规则用它区分「上次封禁之后仍在活动」与「同一批旧证据被再次扫到」。
func HasIpLogsSince(ip string, since int64, excludeUserIds []int) (bool, error) {
	tx := LOG_DB.Table("logs").
		Where("ip = ?", ip).
		Where("created_at > ?", since).
		Where("type IN ?", riskLogTypes())

	var ids []int
	err := riskExcludeUsers(tx, excludeUserIds).Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
}

// HasUserLogsSince 判断某账号在 since 之后是否还有风控统计口径内的日志行。
// 与 HasIpLogsSince 同理:账号的临时禁用到期恢复后,扫描窗口里的旧证据仍然存在,
// 只有恢复之后仍在活动才算新的违规,否则同一批日志会把账号一级级推到永久禁用。
func HasUserLogsSince(userId int, since int64) (bool, error) {
	var ids []int
	err := LOG_DB.Table("logs").
		Where("user_id = ?", userId).
		Where("created_at > ?", since).
		Where("type IN ?", riskLogTypes()).
		Limit(1).
		Pluck("id", &ids).Error
	return len(ids) > 0, err
}

// GetIpAssociatedUserIds 返回某 IP 在窗口内关联的全部用户 ID(供自动封禁使用)。
func GetIpAssociatedUserIds(ip string, hours int) ([]int, error) {
	start := normalizeRiskWindow(hours)

	var userIds []int
	err := LOG_DB.Table("logs").
		Where("created_at >= ?", start).
		Where("type IN ?", riskLogTypes()).
		Where("ip = ?", ip).
		Where("user_id > 0").
		Distinct().
		Pluck("user_id", &userIds).Error
	return userIds, err
}
