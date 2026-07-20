package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

var unsafeUpstreamResponseHeadersLower = map[string]struct{}{
	// Hop-by-hop headers belong to the upstream transport connection and must
	// not be replayed on the downstream connection.
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},

	// An upstream provider must not be able to create or clear cookies scoped
	// to the gateway's domain.
	"set-cookie":  {},
	"set-cookie2": {},

	// Origin-wide browser policies belong to the gateway. Forwarding these
	// would let an upstream mutate persistent connection, storage, reporting,
	// or document security behavior for the gateway's own origin.
	"alt-svc":                             {},
	"clear-site-data":                     {},
	"content-security-policy":             {},
	"content-security-policy-report-only": {},
	"cross-origin-embedder-policy":        {},
	"cross-origin-opener-policy":          {},
	"cross-origin-resource-policy":        {},
	"nel":                                 {},
	"origin-agent-cluster":                {},
	"permissions-policy":                  {},
	"referrer-policy":                     {},
	"report-to":                           {},
	"reporting-endpoints":                 {},
	"strict-transport-security":           {},
	"timing-allow-origin":                 {},
	"x-content-type-options":              {},
	"x-frame-options":                     {},
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. Transport-level, cookie, CORS, and
// locally managed headers are excluded. When the upstream header is
// X-Oneapi-Request-Id, its value is captured for later logging.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	headerName := strings.ToLower(strings.TrimSpace(k))
	if headerName == "" || headerName == "content-length" {
		return false
	}
	if strings.EqualFold(headerName, common.RequestIdKey) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	if _, unsafe := unsafeUpstreamResponseHeadersLower[headerName]; unsafe {
		return false
	}
	// The gateway's CORS middleware owns this policy. Copying an upstream value
	// can produce duplicate or contradictory Access-Control headers.
	if strings.HasPrefix(headerName, "access-control-") {
		return false
	}
	return true
}

// ShouldCopyUpstreamStreamHeader applies the additional exclusions required
// when an adaptor parses and re-encodes an upstream response as SSE.
func ShouldCopyUpstreamStreamHeader(c *gin.Context, k string, v []string) bool {
	if !ShouldCopyUpstreamHeader(c, k, v) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "accept-ranges", "age", "cache-control", "content-digest",
		"content-disposition", "content-encoding", "content-location",
		"content-md5", "content-range", "content-type", "digest", "etag",
		"expires", "last-modified", "repr-digest", "vary", "x-accel-buffering":
		return false
	default:
		return true
	}
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
