package model

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	SharedSessionKindSession = "session"
	SharedSessionKindCache   = "cache"
)

// SharedSessionQuery 是跨账号会话/缓存关联排行的查询参数。
type SharedSessionQuery struct {
	Kind           string // session | cache
	Hours          int
	MinUsers       int
	ExcludeUserIds []int
	Keyword        string
	SortBy         string
	SortOrder      string
	Offset         int
	Limit          int
}

type SharedSessionUserRef struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
}

type SharedSessionTokenRef struct {
	TokenId      int    `json:"token_id"`
	TokenName    string `json:"token_name"`
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
}

// SharedSessionItem 表示同一个客户端会话 ID 或缓存关联键被多个不同账号共用的汇总结果。
type SharedSessionItem struct {
	Kind         string                  `json:"kind"`
	Identifier   string                  `json:"identifier"`
	Sources      []string                `json:"sources"`
	UserCount    int                     `json:"user_count"`
	TokenCount   int                     `json:"token_count"`
	RequestCount int64                   `json:"request_count"`
	ErrorCount   int64                   `json:"error_count"`
	Quota        int64                   `json:"quota"`
	FirstSeen    int64                   `json:"first_seen"`
	LastSeen     int64                   `json:"last_seen"`
	Users        []SharedSessionUserRef  `json:"users"`
	Tokens       []SharedSessionTokenRef `json:"tokens"`
	Ips          []string                `json:"ips"`
}

type sharedSessionLogRow struct {
	Identifier   string `gorm:"column:identifier"`
	Hash         string `gorm:"column:hash"`
	Source       string `gorm:"column:source"`
	Ip           string `gorm:"column:ip"`
	TokenId      int    `gorm:"column:token_id"`
	TokenName    string `gorm:"column:token_name"`
	UserId       int    `gorm:"column:user_id"`
	Username     string `gorm:"column:username"`
	RequestCount int64  `gorm:"column:request_count"`
	ErrorCount   int64  `gorm:"column:error_count"`
	Quota        int64  `gorm:"column:quota"`
	FirstSeen    int64  `gorm:"column:first_seen"`
	LastSeen     int64  `gorm:"column:last_seen"`
}

func sharedSessionColumns(kind string) (idCol, hashCol, sourceCol string, err error) {
	switch kind {
	case SharedSessionKindSession:
		return "session_id", "session_hash", "session_source", nil
	case SharedSessionKindCache:
		return "cache_key", "cache_hash", "cache_source", nil
	default:
		return "", "", "", errors.New("无效的会话关联维度")
	}
}

// GetSharedSessionRanking 统计时间窗口内被多个账号共用的客户端会话 ID 或缓存键。
// 先在日志库按完整窗口聚合出完整关联，再应用关键词筛选与排序分页，避免搜索单个用户时
// 把同一会话里的其余账号过滤掉。
func GetSharedSessionRanking(query SharedSessionQuery) ([]SharedSessionItem, int64, error) {
	idCol, hashCol, sourceCol, err := sharedSessionColumns(query.Kind)
	if err != nil {
		return nil, 0, err
	}
	since := normalizeRiskWindow(query.Hours)
	minUsers := normalizeMultiAccountMinUsers(query.MinUsers)

	tx := LOG_DB.Table("logs").
		Select(fmt.Sprintf(`MIN(%s) AS identifier, %s AS hash, %s AS source,
			ip, token_id, token_name, user_id, username,
			COUNT(*) AS request_count,
			SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS error_count,
			COALESCE(SUM(quota), 0) AS quota,
			MIN(created_at) AS first_seen,
			MAX(created_at) AS last_seen`, idCol, hashCol, sourceCol), LogTypeError).
		Where(hashCol+" <> '' AND user_id > 0").
		Where("type IN ? AND created_at >= ?", riskLogTypes(), since).
		Group(hashCol + ", " + sourceCol + ", ip, token_id, token_name, user_id, username")
	tx = riskExcludeUsers(tx, query.ExcludeUserIds)

	var rows []sharedSessionLogRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	type bucket struct {
		item    SharedSessionItem
		users   map[int]*SharedSessionUserRef
		tokens  map[int]*SharedSessionTokenRef
		ips     map[string]struct{}
		sources map[string]struct{}
	}
	buckets := make(map[string]*bucket)
	for _, row := range rows {
		b := buckets[row.Hash]
		if b == nil {
			b = &bucket{
				item:    SharedSessionItem{Kind: query.Kind, Identifier: row.Identifier},
				users:   map[int]*SharedSessionUserRef{},
				tokens:  map[int]*SharedSessionTokenRef{},
				ips:     map[string]struct{}{},
				sources: map[string]struct{}{},
			}
			buckets[row.Hash] = b
		}
		if b.item.Identifier == "" && row.Identifier != "" {
			b.item.Identifier = row.Identifier
		}
		b.item.RequestCount += row.RequestCount
		b.item.ErrorCount += row.ErrorCount
		b.item.Quota += row.Quota
		if b.item.FirstSeen == 0 || (row.FirstSeen > 0 && row.FirstSeen < b.item.FirstSeen) {
			b.item.FirstSeen = row.FirstSeen
		}
		if row.LastSeen > b.item.LastSeen {
			b.item.LastSeen = row.LastSeen
		}
		if u := b.users[row.UserId]; u == nil {
			b.users[row.UserId] = &SharedSessionUserRef{UserId: row.UserId, Username: row.Username, RequestCount: row.RequestCount}
		} else {
			u.RequestCount += row.RequestCount
			if u.Username == "" {
				u.Username = row.Username
			}
		}
		if row.TokenId > 0 {
			if tok := b.tokens[row.TokenId]; tok == nil {
				b.tokens[row.TokenId] = &SharedSessionTokenRef{
					TokenId:      row.TokenId,
					TokenName:    row.TokenName,
					UserId:       row.UserId,
					Username:     row.Username,
					RequestCount: row.RequestCount,
				}
			} else {
				tok.RequestCount += row.RequestCount
				if tok.TokenName == "" {
					tok.TokenName = row.TokenName
				}
			}
		}
		if row.Ip != "" {
			b.ips[row.Ip] = struct{}{}
		}
		if row.Source != "" {
			b.sources[row.Source] = struct{}{}
		}
	}

	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	items := make([]SharedSessionItem, 0, len(buckets))
	for _, b := range buckets {
		if len(b.users) < minUsers {
			continue
		}
		b.item.UserCount = len(b.users)
		b.item.TokenCount = len(b.tokens)
		b.item.Users = make([]SharedSessionUserRef, 0, len(b.users))
		for _, u := range b.users {
			b.item.Users = append(b.item.Users, *u)
		}
		sort.Slice(b.item.Users, func(i, j int) bool {
			if b.item.Users[i].RequestCount != b.item.Users[j].RequestCount {
				return b.item.Users[i].RequestCount > b.item.Users[j].RequestCount
			}
			return b.item.Users[i].UserId < b.item.Users[j].UserId
		})
		b.item.Tokens = make([]SharedSessionTokenRef, 0, len(b.tokens))
		for _, tok := range b.tokens {
			b.item.Tokens = append(b.item.Tokens, *tok)
		}
		sort.Slice(b.item.Tokens, func(i, j int) bool {
			if b.item.Tokens[i].RequestCount != b.item.Tokens[j].RequestCount {
				return b.item.Tokens[i].RequestCount > b.item.Tokens[j].RequestCount
			}
			return b.item.Tokens[i].TokenId < b.item.Tokens[j].TokenId
		})
		b.item.Ips = make([]string, 0, len(b.ips))
		for ip := range b.ips {
			b.item.Ips = append(b.item.Ips, ip)
		}
		sort.Strings(b.item.Ips)
		b.item.Sources = make([]string, 0, len(b.sources))
		for src := range b.sources {
			b.item.Sources = append(b.item.Sources, src)
		}
		sort.Strings(b.item.Sources)

		if keyword != "" && !sharedSessionMatchesKeyword(b.item, keyword) {
			continue
		}
		items = append(items, b.item)
	}

	desc := !strings.EqualFold(query.SortOrder, "asc")
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if cmp := compareSharedSessionMetric(a, b, query.SortBy); cmp != 0 {
			if desc {
				return cmp > 0
			}
			return cmp < 0
		}
		if a.LastSeen != b.LastSeen {
			return a.LastSeen > b.LastSeen
		}
		return a.Identifier < b.Identifier
	})

	total := int64(len(items))
	limit := normalizeRiskLimit(query.Limit)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []SharedSessionItem{}, total, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], total, nil
}

func compareSharedSessionMetric(a, b SharedSessionItem, sortBy string) int64 {
	switch sortBy {
	case "token_count":
		return int64(a.TokenCount - b.TokenCount)
	case "request_count":
		return a.RequestCount - b.RequestCount
	case "error_count":
		return a.ErrorCount - b.ErrorCount
	case "quota":
		return a.Quota - b.Quota
	case "first_seen":
		return a.FirstSeen - b.FirstSeen
	case "last_seen":
		return a.LastSeen - b.LastSeen
	default:
		if a.UserCount != b.UserCount {
			return int64(a.UserCount - b.UserCount)
		}
		return a.RequestCount - b.RequestCount
	}
}

func sharedSessionMatchesKeyword(item SharedSessionItem, keyword string) bool {
	if strings.Contains(strings.ToLower(item.Identifier), keyword) {
		return true
	}
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		for _, u := range item.Users {
			if u.UserId == id {
				return true
			}
		}
		for _, tok := range item.Tokens {
			if tok.TokenId == id || tok.UserId == id {
				return true
			}
		}
	}
	for _, u := range item.Users {
		if strings.Contains(strings.ToLower(u.Username), keyword) {
			return true
		}
	}
	for _, tok := range item.Tokens {
		if strings.Contains(strings.ToLower(tok.TokenName), keyword) || strings.Contains(strings.ToLower(tok.Username), keyword) {
			return true
		}
	}
	for _, ip := range item.Ips {
		if strings.Contains(strings.ToLower(ip), keyword) {
			return true
		}
	}
	for _, src := range item.Sources {
		if strings.Contains(strings.ToLower(src), keyword) {
			return true
		}
	}
	return false
}

// GetSharedSessionDetailUsers 返回指定会话/缓存标识在时间窗口内的关联账号 ID，供扫描规则复用。
func GetSharedSessionDetailUsers(kind string, identifier string, hours int, excludeUserIds []int) ([]int, error) {
	_, hashCol, _, err := sharedSessionColumns(kind)
	if err != nil {
		return nil, err
	}
	hash := common.SessionIdentifierHash(common.NormalizeSessionIdentifier(identifier))
	if hash == "" {
		return nil, nil
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	if hours <= 0 || hours > riskMaxWindowHours {
		since = normalizeRiskWindow(hours)
	}
	tx := LOG_DB.Table("logs").
		Select("DISTINCT user_id").
		Where(hashCol+" = ? AND user_id > 0 AND type IN ? AND created_at >= ?", hash, riskLogTypes(), since)
	tx = riskExcludeUsers(tx, excludeUserIds)
	var ids []int
	err = tx.Pluck("user_id", &ids).Error
	return ids, err
}
