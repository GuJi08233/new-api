package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioPassthroughPreservesNativeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		mode        int
		contentType string
		body        string
	}{
		{
			"speech", constant.RelayModeAudioSpeech, "application/json",
			"{\n \"model\":\"tts-1\", \"input\":\"hello\", \"unknown_option\":false, \"speed\":0\n}",
		},
		{
			"transcription multipart", constant.RelayModeAudioTranscription, "multipart/form-data; boundary=client-boundary",
			"--client-boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nwhisper-1\r\n" +
				"--client-boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"sample.wav\"\r\n" +
				"Content-Type: audio/wav\r\n\r\nclient-audio-bytes\r\n--client-boundary--\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", tc.contentType)
			defer common.CleanupBodyStorage(c)
			info := &relaycommon.RelayInfo{
				RelayMode: tc.mode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
					IsModelMapped:  true, UpstreamModelName: "must-not-rewrite",
				},
			}
			adaptor := &Adaptor{}
			reader, err := adaptor.ConvertAudioRequest(c, info, dto.AudioRequest{Model: "must-not-rewrite"})
			require.NoError(t, err)
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, tc.body, string(body))
			assert.Equal(t, tc.contentType, c.Request.Header.Get("Content-Type"))
		})
	}
}
