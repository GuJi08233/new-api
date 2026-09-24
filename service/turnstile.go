package service

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ErrTurnstileRejected 表示 Cloudflare 判定令牌无效：过期、已经用过，或者没有通过人机验证。
var ErrTurnstileRejected = errors.New("turnstile token rejected")

var turnstileVerifyUrl = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var turnstileClient = &http.Client{Timeout: 10 * time.Second}

// VerifyTurnstile 向 Cloudflare 校验一个 Turnstile 令牌。令牌只能用一次，需要逐次确认是真人的操作
// (例如每局游戏开局)每次都要带新令牌。Cloudflare 拒绝时返回 ErrTurnstileRejected，其他错误是请求本身失败。
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
		Success bool `json:"success"`
	}
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return err
	}
	if !result.Success {
		return ErrTurnstileRejected
	}
	return nil
}
