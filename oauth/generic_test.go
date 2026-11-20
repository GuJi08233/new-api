package oauth

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericOAuthPKCETokenRequest(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	for _, tt := range []struct {
		name        string
		authStyle   int
		enablePKCE  bool
		verifier    string
		expectError bool
	}{
		{name: "POST credentials", authStyle: AuthStyleInParams, enablePKCE: true, verifier: verifier},
		{name: "Basic credentials", authStyle: AuthStyleInHeader, enablePKCE: true, verifier: verifier},
		{name: "PKCE disabled", authStyle: AuthStyleAutoDetect},
		{name: "missing verifier", authStyle: AuthStyleInParams, enablePKCE: true, expectError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.NoError(t, r.ParseForm())
				assert.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
				assert.Equal(t, "test-code", r.PostForm.Get("code"))
				assert.Equal(t, tt.verifier, r.PostForm.Get("code_verifier"))
				assert.Equal(t, tt.enablePKCE, r.PostForm.Has("code_verifier"))
				id, secret, basic := r.BasicAuth()
				if tt.authStyle == AuthStyleInHeader {
					assert.True(t, basic)
					assert.Equal(t, "client-id", id)
					assert.Equal(t, "client-secret", secret)
					assert.False(t, r.PostForm.Has("client_id"))
					assert.False(t, r.PostForm.Has("client_secret"))
				} else {
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Equal(t, "client-id", r.PostForm.Get("client_id"))
					assert.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
				}
				_, _ = w.Write([]byte(`{"access_token":"test-token"}`))
			}))
			t.Cleanup(server.Close)

			provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
				Slug: "corp", TokenEndpoint: server.URL, AuthStyle: tt.authStyle,
				ClientId: "client-id", ClientSecret: "client-secret", EnablePKCE: tt.enablePKCE,
			})
			if tt.expectError {
				_, err := provider.ExchangeToken(t.Context(), "test-code", nil)
				require.Error(t, err)
				assert.Zero(t, calls.Load())
				return
			}
			token, err := provider.ExchangeTokenWithVerifier(t.Context(), "test-code", tt.verifier)
			require.NoError(t, err)
			assert.Equal(t, "test-token", token.AccessToken)
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}
