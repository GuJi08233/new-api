package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomOAuthPKCEVerificationFlow(t *testing.T) {
	db, router, user := accountSecurityFixture(t)
	address := system_setting.ServerAddress
	system_setting.ServerAddress = "https://app.example.test"
	t.Cleanup(func() { system_setting.ServerAddress = address })

	var tokenCalls atomic.Int32
	var exchangedVerifier atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			assert.Equal(t, "https://app.example.test/oauth/corp-pkce", r.PostFormValue("redirect_uri"))
			exchangedVerifier.Store(r.PostFormValue("code_verifier"))
			_, _ = w.Write([]byte(`{"access_token":"test-token"}`))
		case "/me":
			_, _ = w.Write([]byte(`{"user":{"id":"subject-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	config := &model.CustomOAuthProvider{
		Id: 42, Name: "Corp SSO", Slug: "corp-pkce", Enabled: true, EnablePKCE: true,
		ClientId: "corp-client", AuthorizationEndpoint: server.URL + "/authorize?audience=portal",
		TokenEndpoint: server.URL + "/token", UserInfoEndpoint: server.URL + "/me",
		Scopes: "profile read", UserIdField: "user.id",
	}
	oauth.RegisterOrUpdateCustomProvider(config)
	t.Cleanup(func() { oauth.Unregister(config.Slug) })
	require.NoError(t, db.Create(&model.UserOAuthBinding{
		UserId: user.Id, ProviderId: config.Id, ProviderUserId: "subject-1",
	}).Error)

	login, loginResponse := accountRequest(t, router, http.MethodPost, "/primary", nil, nil, "")
	require.Equal(t, true, loginResponse["success"])
	cookies := login.Result().Cookies()
	digest := strings.Repeat("a", 64)
	startPath := "/api/oauth/state?provider=corp-pkce&verification=true&scope=account.delete&context_hash=" + digest
	start, result := accountRequest(t, router, http.MethodGet, startPath, cookies, nil, "")
	require.Equal(t, true, result["success"], start.Body.String())
	data, ok := result["data"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, data, "code_verifier")
	state, ok := data["state"].(string)
	require.True(t, ok)
	rawURL, ok := data["authorization_url"].(string)
	require.True(t, ok)
	authURL, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "/authorize", authURL.Path)
	query := authURL.Query()
	challenge := query.Get("code_challenge")
	require.NotEmpty(t, challenge)
	assert.Equal(t, url.Values{
		"audience": {"portal"}, "client_id": {"corp-client"}, "response_type": {"code"},
		"redirect_uri": {"https://app.example.test/oauth/corp-pkce"}, "scope": {"profile read"}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "prompt": {"login"},
	}, query)

	callback := "/api/oauth/corp-pkce?code=auth-code&code_verifier=untrusted&state=" + url.QueryEscape(state)
	_, verified := accountRequest(t, router, http.MethodGet, callback, start.Result().Cookies(), nil, "")
	require.Equal(t, true, verified["success"], verified)
	require.EqualValues(t, 1, tokenCalls.Load())
	verifier := exchangedVerifier.Load().(string)
	assert.Regexp(t, `^[A-Za-z0-9._~-]{43,128}$`, verifier)
	assert.NotContains(t, start.Body.String(), verifier)
	sum := sha256.Sum256([]byte(verifier))
	assert.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(sum[:]))

	var usedFlow model.SecurityFlow
	require.NoError(t, db.First(&usedFlow, "id = ?", model.SecurityTokenHash(state)).Error)
	assert.True(t, usedFlow.Consumed)
	assert.Empty(t, usedFlow.Payload)
	_, replay := accountRequest(t, router, http.MethodGet, callback, start.Result().Cookies(), nil, "")
	assert.Equal(t, false, replay["success"])
	assert.EqualValues(t, 1, tokenCalls.Load())

	expired, fresh := accountRequest(t, router, http.MethodGet, startPath, cookies, nil, "")
	require.Equal(t, true, fresh["success"], expired.Body.String())
	expiredState := fresh["data"].(map[string]any)["state"].(string)
	require.NoError(t, db.Model(&model.SecurityFlow{}).Where("id = ?", model.SecurityTokenHash(expiredState)).
		Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	_, denied := accountRequest(t, router, http.MethodGet, "/api/oauth/corp-pkce?code=auth-code&state="+url.QueryEscape(expiredState), expired.Result().Cookies(), nil, "")
	assert.Equal(t, false, denied["success"])
	assert.EqualValues(t, 1, tokenCalls.Load())
}

func TestCustomOAuthPKCEConfiguration(t *testing.T) {
	db, router, _ := accountSecurityFixture(t)
	require.NoError(t, db.AutoMigrate(&model.CustomOAuthProvider{}))
	const slug = "corp-config"
	t.Cleanup(func() { oauth.Unregister(slug) })

	const base = "/api/custom-oauth-provider"
	router.POST(base, CreateCustomOAuthProvider)
	router.PUT(base+"/:id", UpdateCustomOAuthProvider)
	_, created := accountRequest(t, router, http.MethodPost, base, nil, map[string]any{
		"name": "Corp SSO", "slug": slug, "enabled": true,
		"client_id": "corp-client", "client_secret": "corp-secret",
		"authorization_endpoint": "https://idp.example.test/authorize",
		"token_endpoint":         "https://idp.example.test/token",
		"user_info_endpoint":     "https://idp.example.test/me",
	}, "")
	require.Equal(t, true, created["success"], created)
	provider := created["data"].(map[string]any)
	assert.Equal(t, false, provider["enable_pkce"])
	path := base + "/" + strconv.Itoa(int(provider["id"].(float64)))
	statePath := "/api/oauth/state?provider=" + slug

	for _, tt := range []struct {
		name        string
		body        map[string]any
		wantEnabled bool
	}{
		{name: "default"},
		{name: "enable", body: map[string]any{"enable_pkce": true}, wantEnabled: true},
		{name: "omitted flag", body: map[string]any{"name": "Corp Login"}, wantEnabled: true},
		{name: "disable", body: map[string]any{"enable_pkce": false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.body != nil {
				_, updated := accountRequest(t, router, http.MethodPut, path, nil, tt.body, "")
				require.Equal(t, true, updated["success"], updated)
				assert.Equal(t, tt.wantEnabled, updated["data"].(map[string]any)["enable_pkce"])
			}
			_, start := accountRequest(t, router, http.MethodGet, statePath, nil, nil, "")
			require.Equal(t, true, start["success"], start)
			if tt.wantEnabled {
				assert.NotEmpty(t, start["data"].(map[string]any)["authorization_url"])
			} else {
				assert.IsType(t, "", start["data"])
			}
		})
	}
}
