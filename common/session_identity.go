package common

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const RequestSessionIdentityKey = "request_session_identity"

// RequestSessionIdentity 保存客户端原始传入的会话与缓存关联标识，
// 必须在渠道改写之前提取，避免把网关或上游生成的值误当客户端证据。
type RequestSessionIdentity struct {
	SessionID     string
	SessionSource string
	CacheKey      string
	CacheSource   string
}

// NormalizeSessionIdentifier 规整会话/缓存标识，拒绝空值、超长、非法 UTF-8 与控制字符。
func NormalizeSessionIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

// SessionIdentifierHash 用 SHA-256 归一化为区分大小写的定长哈希，
// 避免在 MySQL 默认大小写不敏感排序规则下把不同大小写的会话混成同一个。
func SessionIdentifierHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// ExtractRequestSessionIdentity 从原始请求头与 JSON 请求体提取客户端会话 ID 与缓存关联键。
func ExtractRequestSessionIdentity(headers http.Header, body []byte) RequestSessionIdentity {
	var identity RequestSessionIdentity
	if headers == nil {
		headers = http.Header{}
	}
	opencode := headers.Get("x-opencode-client") != "" || headers.Get("x-opencode-project") != "" ||
		headers.Get("x-opencode-session") != "" ||
		strings.HasPrefix(strings.ToLower(headers.Get("User-Agent")), "opencode/") ||
		headers.Get("x-opencode-session-id") != ""
	for _, name := range []string{
		"x-opencode-session-id",
		"x-deepseek-harness-session-id",
		"x-claude-code-session-id",
		"x-astrbot-conversation-id",
		"session-id",
		"session_id",
		"x-session-id",
	} {
		if name == "x-session-id" && opencode {
			continue
		}
		if value := NormalizeSessionIdentifier(headers.Get(name)); value != "" {
			identity.SessionID, identity.SessionSource = value, "header:"+name
			break
		}
	}
	for _, name := range []string{"x-opencode-session", "x-session-affinity"} {
		if value := NormalizeSessionIdentifier(headers.Get(name)); value != "" {
			identity.CacheKey, identity.CacheSource = value, "header:"+name
			break
		}
	}
	validBody := len(body) > 0 && gjson.ValidBytes(body)
	if identity.CacheKey == "" && validBody {
		key := gjson.GetBytes(body, "prompt_cache_key")
		if key.Type == gjson.String {
			identity.CacheKey = NormalizeSessionIdentifier(key.Str)
			if identity.CacheKey != "" {
				identity.CacheSource = "body:prompt_cache_key"
			}
		}
	}
	if identity.CacheKey == "" && opencode {
		if value := NormalizeSessionIdentifier(headers.Get("x-session-id")); value != "" {
			identity.CacheKey, identity.CacheSource = value, "header:x-session-id"
		}
	}
	if !validBody || identity.SessionID != "" {
		return identity
	}

	metadata := gjson.GetBytes(body, "metadata.user_id")
	if metadata.Type != gjson.String {
		return identity
	}
	var embedded struct {
		SessionID string `json:"session_id"`
	}
	if UnmarshalJsonStr(metadata.Str, &embedded) == nil {
		identity.SessionID = NormalizeSessionIdentifier(embedded.SessionID)
	} else if strings.HasPrefix(metadata.Str, "user_") {
		account := strings.Index(metadata.Str, "_account_")
		if account > len("user_") {
			_, session, found := strings.Cut(metadata.Str[account+len("_account_"):], "_session_")
			if found {
				identity.SessionID = NormalizeSessionIdentifier(session)
			}
		}
	}
	if identity.SessionID != "" {
		identity.SessionSource = "body:metadata.user_id.session_id"
		return identity
	}
	// Kimi CLI 将会话 ID 直接写入 metadata.user_id；仅对明确识别的客户端放行，
	// 避免把通用 Anthropic SDK 里的普通用户标识误当会话 ID。
	userAgent := strings.ToLower(strings.TrimSpace(headers.Get("User-Agent")))
	kimi := strings.HasPrefix(userAgent, "kimicli/") || strings.HasPrefix(userAgent, "kimi-code-desktop/") ||
		strings.EqualFold(strings.TrimSpace(headers.Get("X-Msh-Platform")), "kimi_cli")
	plain := NormalizeSessionIdentifier(metadata.Str)
	structured := strings.HasPrefix(plain, "{") || strings.HasPrefix(plain, "[") ||
		(strings.HasPrefix(plain, "user_") && strings.Contains(plain, "_account_"))
	if kimi && plain != "" && !structured {
		identity.SessionID, identity.SessionSource = plain, "body:metadata.user_id"
	}
	return identity
}
