package channel

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestPassthroughPreservesEncodingAndDecodesForAccounting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	for _, encoding := range []string{"", "gzip", "deflate", "br", "zstd", "br, gzip"} {
		t.Run(encoding, func(t *testing.T) {
			raw := `{"usage":{"prompt_tokens":10,"completion_tokens":2}}`
			isStream := encoding == "gzip"
			if isStream {
				raw = "data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"
			}
			received := make(chan http.Header, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received <- r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				if isStream {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				var output io.Writer = w
				var closers []io.Closer
				if encoding != "" {
					w.Header().Set("Content-Encoding", encoding)
					encodings := strings.Split(encoding, ", ")
					for i := len(encodings) - 1; i >= 0; i-- {
						var writer io.WriteCloser
						switch encodings[i] {
						case "gzip":
							writer = gzip.NewWriter(output)
						case "deflate":
							writer = zlib.NewWriter(output)
						case "br":
							writer = brotli.NewWriter(output)
						case "zstd":
							var err error
							writer, err = zstd.NewWriter(output, zstd.WithEncoderConcurrency(1))
							if err != nil {
								t.Error(err)
								return
							}
						}
						output = writer
						closers = append([]io.Closer{writer}, closers...)
					}
				}
				_, err := io.WriteString(output, raw)
				assert.NoError(t, err)
				for _, closer := range closers {
					assert.NoError(t, closer.Close())
				}
			}))
			defer upstream.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
			if encoding != "" {
				c.Request.Header.Add("Accept-Encoding", "gzip")
				c.Request.Header.Add("Accept-Encoding", "deflate, br, zstd")
			}
			request, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("{}"))
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{
				IsStream: isStream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
				},
			}
			resp, err := DoRequest(c, request, info)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, raw, string(body))
			assert.Empty(t, resp.Header.Get("Content-Encoding"))
			got := <-received
			assert.Equal(t, c.Request.Header.Values("Accept-Encoding"), got.Values("Accept-Encoding"))
		})
	}
}

func TestDoRequestPassthroughRejectsInvalidResponseEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	for _, encoding := range []string{"gzip", "unsupported"} {
		t.Run(encoding, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", encoding)
				_, _ = io.WriteString(w, "not a compressed response")
			}))
			defer upstream.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			request, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
			}}
			resp, err := DoRequest(c, request, info)
			require.ErrorContains(t, err, "decode upstream response failed")
			assert.Nil(t, resp)
		})
	}
}
