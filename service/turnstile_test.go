package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 只有 Cloudflare 明确判定通过的令牌才算通过；请求里带上站点密钥、令牌和用户 IP，供 Cloudflare 核对。
func TestVerifyTurnstileAcceptsOnlyTokensCloudflarePasses(t *testing.T) {
	var form map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		form = map[string]string{"secret": r.PostForm.Get("secret"), "response": r.PostForm.Get("response"), "remoteip": r.PostForm.Get("remoteip")}
		if r.PostForm.Get("response") == "good-token" {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["timeout-or-duplicate"]}`))
	}))
	defer server.Close()
	previousUrl, previousSecret := turnstileVerifyUrl, common.TurnstileSecretKey
	turnstileVerifyUrl, common.TurnstileSecretKey = server.URL, "site-secret"
	t.Cleanup(func() { turnstileVerifyUrl, common.TurnstileSecretKey = previousUrl, previousSecret })

	require.NoError(t, VerifyTurnstile("good-token", "203.0.113.7"))
	assert.Equal(t, map[string]string{"secret": "site-secret", "response": "good-token", "remoteip": "203.0.113.7"}, form)
	assert.ErrorIs(t, VerifyTurnstile("used-token", "203.0.113.7"), ErrTurnstileRejected)
}

// 站点密钥没填或填错时 Cloudflare 同样回 success:false，但这不是用户的问题：返回的错误不能当作令牌被拒，
// 调用方据此记日志，管理员才查得出为什么所有人都过不了验证。
func TestVerifyTurnstileSeparatesSiteErrorsFromRejectedTokens(t *testing.T) {
	cases := map[string]bool{
		`{"success":false,"error-codes":["invalid-input-response"]}`: true,
		`{"success":false,"error-codes":["missing-input-response"]}`: true,
		`{"success":false}`: true,
		`{"success":false,"error-codes":["missing-input-secret"]}`: false,
		`{"success":false,"error-codes":["invalid-input-secret"]}`: false,
		`{"success":false,"error-codes":["internal-error"]}`:       false,
	}
	for body, rejected := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		previousUrl := turnstileVerifyUrl
		turnstileVerifyUrl = server.URL
		err := VerifyTurnstile("token", "203.0.113.7")
		turnstileVerifyUrl = previousUrl
		server.Close()
		require.Error(t, err, body)
		assert.Equal(t, rejected, errors.Is(err, ErrTurnstileRejected), body)
		assert.Equal(t, !rejected, errors.Is(err, ErrTurnstileSiteError), body)
	}
}
