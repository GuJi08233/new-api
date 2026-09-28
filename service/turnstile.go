package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ErrTurnstileRejected 表示 Cloudflare 判定令牌无效：过期、已经用过，或者没有通过人机验证。
var ErrTurnstileRejected = errors.New("turnstile token rejected")

// ErrTurnstileSiteError 表示 Cloudflare 拒绝的是站点这一边：站点密钥没填或填错、Cloudflare 内部出错。不是用户的问题，
// 调用方要记进日志，但不必把原因显示给用户。
var ErrTurnstileSiteError = errors.New("turnstile verification failed on the site side")

var turnstileVerifyUrl = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var turnstileClient = &http.Client{Timeout: 10 * time.Second}

// VerifyTurnstile 向 Cloudflare 校验一个 Turnstile 令牌。令牌只能用一次，需要逐次确认是真人的操作
// (例如每局游戏开局)每次都要带新令牌。只有令牌本身的问题(缺失、无效、过期或已用过)返回 ErrTurnstileRejected；
// 站点密钥没填或填错、Cloudflare 内部出错时返回别的错误，调用方要记进日志，否则所有人都过不了验证却查不出原因。
func VerifyTurnstile(token string, remoteIp string) error {
	resp, err := turnstileClient.PostForm(turnstileVerifyUrl, url.Values{
		"secret":   {common.TurnstileSecretKey},
		"response": {token},
		"remoteip": {remoteIp},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return err
	}
	if result.Success {
		return nil
	}
	for _, code := range result.ErrorCodes {
		switch code {
		case "missing-input-response", "invalid-input-response", "timeout-or-duplicate":
		default:
			return fmt.Errorf("%w: %s", ErrTurnstileSiteError, strings.Join(result.ErrorCodes, ", "))
		}
	}
	return ErrTurnstileRejected
}
