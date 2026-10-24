package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedirectCredentialBoundary(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.EnableSSRFProtection = false

	for _, tc := range []struct {
		name, origin, target, intermediate string
		body                               bool
		keepCredentials                    bool
		wantError                          string
	}{
		{name: "same origin", origin: "https://api.example/a", target: "https://api.example/b", keepCredentials: true},
		{name: "explicit default port", origin: "https://api.example/a", target: "https://API.example:443/b", keepCredentials: true},
		{name: "HTTPS upgrade", origin: "http://api.example/a", target: "https://api.example/b", keepCredentials: true},
		{name: "different host", origin: "https://api.example/a", target: "https://cdn.example/b"},
		{name: "subdomain", origin: "https://api.example/a", target: "https://cdn.api.example/b"},
		{name: "different port", origin: "https://api.example/a", target: "https://api.example:8443/b"},
		{name: "return after untrusted hop", origin: "https://api.example/a", intermediate: "https://cdn.example/b", target: "https://api.example/c"},
		{name: "HTTPS downgrade", origin: "https://api.example/a", target: "http://api.example/b", wantError: "downgrade HTTPS"},
		{name: "cross origin body replay", origin: "https://api.example/a", target: "https://cdn.example/b", body: true, wantError: "replay a body"},
		{name: "same origin body replay", origin: "https://api.example/a", target: "https://api.example/b", body: true, keepCredentials: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial, err := http.NewRequest(http.MethodGet, tc.origin, nil)
			require.NoError(t, err)
			var body io.Reader
			if tc.body {
				body = strings.NewReader("private task data")
			}
			next, err := http.NewRequest(http.MethodGet, tc.target, body)
			require.NoError(t, err)
			next.Header.Set("Authorization", "Bearer fictional")
			next.Header.Set("x-goog-api-key", "fictional")
			next.Header.Set("X-Private-Credential", "fictional")
			next.Header.Set("Referer", tc.origin)
			next.Header.Set("Accept", "video/mp4")
			next.Header.Set("Accept-Language", "en")
			next.URL.User = url.UserPassword("fictional", "fictional")
			via := []*http.Request{initial}
			if tc.intermediate != "" {
				intermediate, err := http.NewRequest(http.MethodGet, tc.intermediate, nil)
				require.NoError(t, err)
				via = append(via, intermediate)
			}
			err = checkRedirect(next, via)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			if tc.keepCredentials {
				assert.Equal(t, "fictional", next.Header.Get("x-goog-api-key"))
				assert.Equal(t, "Bearer fictional", next.Header.Get("Authorization"))
				return
			}
			assert.Empty(t, next.Header.Get("x-goog-api-key"))
			assert.Empty(t, next.Header.Get("X-Private-Credential"))
			assert.Empty(t, next.Header.Get("Authorization"))
			assert.Empty(t, next.Header.Get("Referer"))
			assert.Nil(t, next.URL.User)
			assert.Empty(t, next.Host)
			assert.Equal(t, "video/mp4", next.Header.Get("Accept"))
			assert.Equal(t, "en", next.Header.Get("Accept-Language"))
		})
	}
}

func TestRedirectMediaRemainsAnonymousAcrossHosts(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.EnableSSRFProtection = false
	received := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		_, _ = io.WriteString(w, "media data")
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(source.Close)
	request, err := http.NewRequest(http.MethodGet, strings.Replace(source.URL, "127.0.0.1", "localhost", 1), nil)
	require.NoError(t, err)
	request.Header.Set("x-goog-api-key", "fictional")
	request.Header.Set("X-Private-Credential", "fictional")
	request.Header.Set("Accept", "video/mp4")
	client := &http.Client{CheckRedirect: checkRedirect}
	response, err := client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "media data", string(body))
	headers := <-received
	assert.Empty(t, headers.Get("x-goog-api-key"))
	assert.Empty(t, headers.Get("X-Private-Credential"))
	assert.Equal(t, "video/mp4", headers.Get("Accept"))
}

func TestRedirectStillEnforcesSSRFAndHopLimit(t *testing.T) {
	configureSSRFTestFetchSetting(t)
	initial, err := http.NewRequest(http.MethodGet, "http://8.8.8.8/", nil)
	require.NoError(t, err)
	privateTarget, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	require.NoError(t, err)
	for _, policy := range []func(*http.Request, []*http.Request) error{checkRedirect, checkProtectedFetchRedirect} {
		require.ErrorContains(t, policy(privateTarget, []*http.Request{initial}), "private IP address not allowed")
		chain := make([]*http.Request, 10)
		for i := range chain {
			chain[i] = initial
		}
		require.ErrorContains(t, policy(initial, chain), "10 redirects")
	}
}

func TestRedirectCannotRestoreCredentialsAfterUntrustedHop(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.EnableSSRFProtection = false
	received := make(chan http.Header, 2)
	var origin string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		http.Redirect(w, r, origin+"/finish", http.StatusFound)
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(source.Close)
	origin = strings.Replace(source.URL, "127.0.0.1", "localhost", 1)
	request, err := http.NewRequest(http.MethodGet, origin+"/start", nil)
	require.NoError(t, err)
	request.Header.Set("x-goog-api-key", "fictional")
	client := &http.Client{CheckRedirect: checkRedirect}
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Len(t, received, 2)
	assert.Empty(t, (<-received).Get("x-goog-api-key"))
	assert.Empty(t, (<-received).Get("x-goog-api-key"))
}

func TestRedirectDoesNotReplayPrivateBodyAcrossOrigins(t *testing.T) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	setting.EnableSSRFProtection = false
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, code)
			}))
			t.Cleanup(source.Close)
			request, err := http.NewRequest(http.MethodPost, source.URL, strings.NewReader("private task body"))
			require.NoError(t, err)
			client := &http.Client{CheckRedirect: checkRedirect}
			_, err = client.Do(request)
			require.ErrorContains(t, err, "replay a body")
			assert.Zero(t, requests.Load())
		})
	}
}
