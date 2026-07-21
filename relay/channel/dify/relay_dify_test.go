package dify

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadDifyFileAppliesCustomHeadersAndPreservesUploadHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	type uploadRequest struct {
		header   http.Header
		parseErr error
		user     string
	}
	received := make(chan uploadRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parseErr := r.ParseMultipartForm(1 << 20)
		received <- uploadRequest{header: r.Header.Clone(), parseErr: parseErr, user: r.FormValue("user")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-id"}`))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-token")
	c.Request.Header.Set("X-Trace-Id", "trace-dify")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "provider-token",
			HeadersOverride: map[string]any{
				"Authorization": "Bearer custom-token",
				"Content-Type":  "application/json",
			},
			ChannelSetting: dto.ChannelSettings{PassThroughHeadersEnabled: true},
		},
	}
	media := dto.MediaContent{
		Type: dto.ContentTypeImageURL,
		ImageUrl: &dto.MessageImageUrl{
			Url:      base64.StdEncoding.EncodeToString([]byte("image-data")),
			MimeType: "image/png",
		},
	}

	file := uploadDifyFile(c, info, "user-id", media)
	require.NotNil(t, file)
	assert.Equal(t, "file-id", file.UploadFileId)

	request := <-received
	require.NoError(t, request.parseErr)
	assert.Equal(t, "user-id", request.user)
	assert.Equal(t, "Bearer custom-token", request.header.Get("Authorization"))
	assert.True(t, strings.HasPrefix(request.header.Get("Content-Type"), "multipart/form-data; boundary="))
	assert.Equal(t, "trace-dify", request.header.Get("X-Trace-Id"))
}
