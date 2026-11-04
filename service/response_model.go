package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// CaptureResponseModelName observes only raw upstream payloads. Calling it on
// converted or generated responses could mistake a request model for a model
// actually reported by the provider. Events without a model preserve the last
// observed value, since usage and completion events often omit it.
func CaptureResponseModelName(c *gin.Context, data []byte) {
	if c == nil || !bytes.Contains(data, modelKeyMarker) || common.GetJsonType(data) != "object" || !gjson.ValidBytes(data) {
		return
	}
	for _, path := range [...]string{"model", "message.model", "response.model", "modelVersion", "session.model"} {
		value := gjson.GetBytes(data, path)
		if value.Type == gjson.String && strings.TrimSpace(value.Str) != "" {
			common.SetContextKey(c, constant.ContextKeyResponseModelName, strings.Clone(value.Str))
			return
		}
	}
}

// ReadResponseBody records the model before an adaptor can discard or replace
// it, using the same body bytes the adaptor already needs to read.
func ReadResponseBody(c *gin.Context, resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	CaptureResponseModelName(c, body)
	return body, err
}
