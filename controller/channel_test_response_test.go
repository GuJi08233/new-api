package controller

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTestUpstreamModel(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"chat", `{"model":"actual-model-20260918","choices":[]}`, "actual-model-20260918"},
		{"responses", `{"object":"response","model":"resolved-model"}`, "resolved-model"},
		{"anthropic", `{"type":"message","model":"claude-resolved"}`, "claude-resolved"},
		{"gemini", `{"modelVersion":"gemini-resolved"}`, "gemini-resolved"},
		{"wrapped task", `{"data":{"model":"video-resolved"}}`, "video-resolved"},
		{"result", `{"result":{"modelVersion":"resolved-version"}}`, "resolved-version"},
		{"array", `[null,{},{"model":""},{"modelVersion":"first-model"},{"model":"second-model"}]`, "first-model"},
		{"empty body", "", ""},
		{"absent", `{"choices":[]}`, ""},
		{"whitespace", `{"model":" actual-model "}`, "actual-model"},
		{"null fallback", `{"model":null,"modelVersion":"actual-version"}`, "actual-version"},
		{"ignore generated", `{"choices":[{"message":{"model":"generated-model"}}],"request":{"model":"request-echo"}}`, ""},
		{"chat stream", "data: {\"model\":\"first-model\"}\n\ndata: {\"model\":\"second-model\"}\n\ndata: [DONE]\n\n", "first-model"},
		{"responses stream", "event: response.created\ndata: {\"response\":{\"model\":\"response-model\"}}\n\n", "response-model"},
		{"anthropic stream", "event: message_start\ndata: {\"message\":{\"model\":\"claude-model\"}}\n\n", "claude-model"},
		{"gemini stream", "data: {\"modelVersion\":\"gemini-model\"}\n\n", "gemini-model"},
		{"crlf", "data: {\"model\":\"actual-model\"}\r\n\r\n", "actual-model"},
		{"multiline event", "data: {\n: comment\ndata: \"model\":\"actual-model\"\ndata: }\n\n", "actual-model"},
		{"large event", "data: {\"content\":\"" + strings.Repeat("x", 70<<10) + "\",\"model\":\"actual-model\"}\n\n", "actual-model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractTestUpstreamModel([]byte(tt.body)))
		})
	}
}

func TestCaptureTestUpstreamResponsePreservesBodyAndErrors(t *testing.T) {
	body := "event: message_start\r\ndata: {\"message\":{\"model\":\"resolved-model\"}}\r\n\r\ndata: [DONE]\r\n\r\n"
	resp := &http.Response{Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(body)))}
	capture := captureTestUpstreamResponse(resp)
	assert.Empty(t, capture.bytes())
	read, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, body, string(read))
	assert.Equal(t, "resolved-model", extractTestUpstreamModel(capture.bytes()))

	errResp := &http.Response{Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}
	errCapture := captureTestUpstreamResponse(errResp)
	_, err = io.ReadAll(errResp.Body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Empty(t, extractTestUpstreamModel(errCapture.bytes()))
	assert.Empty(t, extractTestUpstreamModel(captureTestUpstreamResponse(nil).bytes()))
}
