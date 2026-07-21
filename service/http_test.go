package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyUpstreamHeadersFiltersDynamicHopAndGatewayHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer.Header().Set("X-New-Api-Version", "gateway-version")

	src := http.Header{
		"Connection":        []string{"keep-alive, X-Upstream-Hop"},
		"X-Upstream-Hop":    []string{"must-not-forward"},
		"Auth-Version":      []string{"upstream-auth-version"},
		"Cache-Version":     []string{"upstream-cache-version"},
		"X-New-Api-Version": []string{"upstream-version"},
		"Set-Cookie":        []string{"session=upstream"},
		"Content-Type":      []string{"application/json"},
		"X-Upstream-Meta":   []string{"first", "second"},
	}

	copied := CopyUpstreamHeaders(c, c.Writer.Header(), src, false)

	assert.Equal(t, "gateway-version", c.Writer.Header().Get("X-New-Api-Version"))
	assert.Empty(t, c.Writer.Header().Get("Auth-Version"))
	assert.Empty(t, c.Writer.Header().Get("Cache-Version"))
	assert.Empty(t, c.Writer.Header().Get("X-Upstream-Hop"))
	assert.Empty(t, c.Writer.Header().Values("Set-Cookie"))
	assert.Equal(t, "application/json", c.Writer.Header().Get("Content-Type"))
	assert.Equal(t, []string{"first", "second"}, c.Writer.Header().Values("X-Upstream-Meta"))
	assert.ElementsMatch(t, []string{"Content-Type", "X-Upstream-Meta"}, copied)
}

func TestCopyUpstreamHeadersFiltersCredentialsAndReverseProxyControls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name        string
		transformed bool
	}{
		{name: "raw", transformed: false},
		{name: "transformed", transformed: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			src := http.Header{
				"Authorization":            []string{"Bearer upstream-secret"},
				"Proxy-Authorization":      []string{"Basic proxy-secret"},
				"New-Api-User":             []string{"42"},
				"X-Api-Key":                []string{"upstream-api-key"},
				"X-Goog-Api-Key":           []string{"google-api-key"},
				"Mj-Api-Secret":            []string{"midjourney-secret"},
				"X-Accel-Redirect":         []string{"/private/internal-file"},
				"X-Accel-Limit-Rate":       []string{"1"},
				"X-Sendfile":               []string{"/etc/passwd"},
				"X-Lighttpd-Send-File":     []string{"/etc/shadow"},
				"X-Safe-Upstream-Metadata": []string{"safe"},
			}

			CopyUpstreamHeaders(c, c.Writer.Header(), src, testCase.transformed)

			for _, name := range []string{
				"Authorization",
				"Proxy-Authorization",
				"New-Api-User",
				"X-Api-Key",
				"X-Goog-Api-Key",
				"Mj-Api-Secret",
				"X-Accel-Redirect",
				"X-Accel-Limit-Rate",
				"X-Sendfile",
				"X-Lighttpd-Send-File",
			} {
				assert.Empty(t, c.Writer.Header().Values(name), "%s must not be copied", name)
			}
			assert.Equal(t, "safe", c.Writer.Header().Get("X-Safe-Upstream-Metadata"))
		})
	}
}

func TestIOCopyBytesGracefullyDropsOriginalEntityMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	src := &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			"Content-Type":     []string{"application/octet-stream"},
			"Content-Encoding": []string{"br"},
			"Content-Language": []string{"en"},
			"Content-Range":    []string{"bytes 0-4/5"},
			"Digest":           []string{"sha-256=upstream"},
			"Etag":             []string{`"upstream"`},
			"X-Upstream-Meta":  []string{"safe"},
		},
	}
	body := []byte(`{"ok":true}`)

	IOCopyBytesGracefully(c, src, body)

	result := recorder.Result()
	defer result.Body.Close()
	responseBody := recorder.Body.Bytes()
	require.Equal(t, http.StatusCreated, result.StatusCode)
	assert.Equal(t, body, responseBody)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))
	assert.Equal(t, "11", result.Header.Get("Content-Length"))
	assert.Empty(t, result.Header.Get("Content-Encoding"))
	assert.Empty(t, result.Header.Get("Content-Language"))
	assert.Empty(t, result.Header.Get("Content-Range"))
	assert.Empty(t, result.Header.Get("Digest"))
	assert.Empty(t, result.Header.Get("Etag"))
	assert.Equal(t, "safe", result.Header.Get("X-Upstream-Meta"))
}

func TestIOCopyRawBytesGracefullyKeepsSafeEntityMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	src := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"audio/mpeg"},
			"Etag":         []string{`"audio-entity"`},
			"Connection":   []string{"X-Upstream-Hop"},
			"X-Upstream-Hop": []string{
				"must-not-forward",
			},
		},
	}
	body := []byte("audio")

	IOCopyRawBytesGracefully(c, src, body)

	result := recorder.Result()
	defer result.Body.Close()
	assert.Equal(t, "audio/mpeg", result.Header.Get("Content-Type"))
	assert.Equal(t, `"audio-entity"`, result.Header.Get("Etag"))
	assert.Empty(t, result.Header.Get("X-Upstream-Hop"))
	assert.Equal(t, body, recorder.Body.Bytes())
}
