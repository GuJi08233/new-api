package service

import (
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
