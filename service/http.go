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
	"proxy-connection":    {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},

	// Credential headers are request-scoped secrets. They are not meaningful
	// response metadata and must not be reflected back to gateway clients by a
	// misconfigured or debugging upstream.
	"authorization":  {},
	"mj-api-secret":  {},
	"new-api-user":   {},
	"x-api-key":      {},
	"x-goog-api-key": {},

	// An upstream provider must not be able to create or clear cookies scoped
	// to the gateway's domain.
	"set-cookie":  {},
	"set-cookie2": {},

	// Reverse proxies can interpret these response headers as commands to
	// serve local files instead of forwarding the gateway response.
	"x-lighttpd-send-file": {},
	"x-sendfile":           {},

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

var gatewayManagedResponseHeadersLower = map[string]struct{}{
	"auth-version":      {},
	"cache-version":     {},
	"x-new-api-version": {},
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
	if _, managed := gatewayManagedResponseHeadersLower[headerName]; managed {
		return false
	}
	// The gateway's CORS middleware owns this policy. Copying an upstream value
	// can produce duplicate or contradictory Access-Control headers.
	if strings.HasPrefix(headerName, "access-control-") {
		return false
	}
	// Nginx treats X-Accel-* response fields as control instructions (for
	// example, X-Accel-Redirect can trigger an internal redirect). Keep all of
	// them gateway-owned rather than accepting commands from an AI upstream.
	if strings.HasPrefix(headerName, "x-accel-") {
		return false
	}
	if strings.HasPrefix(headerName, "sec-websocket-") {
		return false
	}
	return true
}

// ShouldCopyUpstreamStreamHeader applies the additional exclusions required
// whenever an adaptor parses and re-encodes an upstream response entity.
func ShouldCopyUpstreamStreamHeader(c *gin.Context, k string, v []string) bool {
	if !ShouldCopyUpstreamHeader(c, k, v) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "accept-ranges", "age", "cache-control", "content-digest",
		"content-disposition", "content-encoding", "content-language", "content-location",
		"content-md5", "content-range", "content-type", "digest", "etag",
		"expires", "last-modified", "repr-digest", "vary", "x-accel-buffering":
		return false
	default:
		return true
	}
}

// CopyUpstreamHeaders copies safe upstream response metadata into dst. When
// transformed is true, representation metadata tied to the original upstream
// body is excluded because the gateway is writing a different entity.
func CopyUpstreamHeaders(c *gin.Context, dst, src http.Header, transformed bool) []string {
	if dst == nil || src == nil {
		return nil
	}

	connectionHeaderNames := make(map[string]struct{})
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" {
				connectionHeaderNames[name] = struct{}{}
			}
		}
	}

	copiedNames := make([]string, 0, len(src))
	for name, values := range src {
		headerNameLower := strings.ToLower(strings.TrimSpace(name))
		if _, declaredHopByHop := connectionHeaderNames[headerNameLower]; declaredHopByHop {
			continue
		}

		var shouldCopy bool
		if transformed {
			shouldCopy = ShouldCopyUpstreamStreamHeader(c, name, values)
		} else {
			shouldCopy = ShouldCopyUpstreamHeader(c, name, values)
		}
		if !shouldCopy {
			continue
		}

		copiedValues := make([]string, 0, len(values))
		for _, value := range values {
			if value != "" {
				copiedValues = append(copiedValues, value)
			}
		}
		if len(copiedValues) == 0 {
			continue
		}

		canonicalName := http.CanonicalHeaderKey(name)
		if len(dst.Values(canonicalName)) > 0 {
			continue
		}
		dst[canonicalName] = copiedValues
		copiedNames = append(copiedNames, canonicalName)
	}
	return copiedNames
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	ioCopyBytesGracefully(c, src, data, true)
}

// IOCopyRawBytesGracefully writes an unmodified upstream entity while
// retaining safe representation metadata such as Content-Type and ETag.
func IOCopyRawBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	ioCopyBytesGracefully(c, src, data, false)
}

func ioCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte, transformed bool) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		CopyUpstreamHeaders(c, c.Writer.Header(), src.Header, transformed)
	}
	if transformed && c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", "application/json")
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
