package replicate

import (
	"bytes"
	"mime/multipart"
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

func TestUploadFileFromFormAppliesCustomHeadersAndPreservesUploadHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	type uploadRequest struct {
		header      http.Header
		parseErr    error
		formFileErr error
		filename    string
	}
	received := make(chan uploadRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parseErr := r.ParseMultipartForm(1 << 20)
		file, fileHeader, formFileErr := r.FormFile("content")
		if file != nil {
			_ = file.Close()
		}
		filename := ""
		if fileHeader != nil {
			filename = fileHeader.Filename
		}
		received <- uploadRequest{
			header:      r.Header.Clone(),
			parseErr:    parseErr,
			formFileErr: formFileErr,
			filename:    filename,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"urls":{"get":"https://files.example/image.png"}}`))
	}))
	defer server.Close()

	var inputBody bytes.Buffer
	inputWriter := multipart.NewWriter(&inputBody)
	part, err := inputWriter.CreateFormFile("image", "image.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-data"))
	require.NoError(t, err)
	require.NoError(t, inputWriter.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &inputBody)
	c.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())
	c.Request.Header.Set("Authorization", "Bearer client-token")
	c.Request.Header.Set("X-Trace-Id", "trace-replicate")
	require.NoError(t, c.Request.ParseMultipartForm(1<<20))
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

	fileURL, err := uploadFileFromForm(c, info, "image")
	require.NoError(t, err)
	assert.Equal(t, "https://files.example/image.png", fileURL)

	request := <-received
	require.NoError(t, request.parseErr)
	require.NoError(t, request.formFileErr)
	assert.Equal(t, "image.png", request.filename)
	assert.Equal(t, "Bearer custom-token", request.header.Get("Authorization"))
	assert.True(t, strings.HasPrefix(request.header.Get("Content-Type"), "multipart/form-data; boundary="))
	assert.Equal(t, "trace-replicate", request.header.Get("X-Trace-Id"))
}
