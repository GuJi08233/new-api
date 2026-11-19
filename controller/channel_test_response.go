package controller

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

const maxChannelTestCaptureBytes = 512 << 10

type testResponseCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *testResponseCapture) writeChunk(p []byte) {
	if c == nil || len(p) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := maxChannelTestCaptureBytes - c.buf.Len()
	if remaining <= 0 {
		return
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = c.buf.Write(p)
}

func (c *testResponseCapture) bytes() []byte {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...)
}

type testResponseTeeCloser struct {
	src     io.ReadCloser
	capture *testResponseCapture
}

func (r *testResponseTeeCloser) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.capture.writeChunk(p[:n])
	}
	return n, err
}

func (r *testResponseTeeCloser) Close() error {
	return r.src.Close()
}

// captureTestUpstreamResponse records the raw upstream response bytes as the
// adaptor reads resp.Body without eagerly buffering or altering the stream.
func captureTestUpstreamResponse(resp *http.Response) *testResponseCapture {
	capture := &testResponseCapture{}
	if resp == nil || resp.Body == nil {
		return capture
	}
	resp.Body = &testResponseTeeCloser{
		src:     resp.Body,
		capture: capture,
	}
	return capture
}

func extractTestUpstreamModel(body []byte) string {
	if gjson.ValidBytes(body) {
		return extractTestModelJSON(body)
	}

	var event []byte
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			if name := extractTestModelJSON(event); name != "" {
				return name
			}
			event = event[:0]
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		if len(event) > 0 {
			event = append(event, '\n')
		}
		event = append(event, bytes.TrimPrefix(line[len("data:"):], []byte{' '})...)
	}
	return extractTestModelJSON(event)
}

func extractTestModelJSON(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	value := gjson.ParseBytes(body)
	if !value.IsArray() {
		return extractTestModelMetadata(value)
	}
	var name string
	value.ForEach(func(_, item gjson.Result) bool {
		name = extractTestModelMetadata(item)
		return name == ""
	})
	return name
}

func extractTestModelMetadata(value gjson.Result) string {
	if !value.IsObject() {
		return ""
	}
	for _, path := range []string{
		"model", "modelVersion", "response.model", "response.modelVersion",
		"message.model", "data.model", "result.model", "result.modelVersion",
	} {
		field := value.Get(path)
		if field.Type == gjson.String {
			if name := strings.TrimSpace(field.String()); name != "" {
				return name
			}
		}
	}
	return ""
}
