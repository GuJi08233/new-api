package helper

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const eventStreamHeadersSetKey = "event_stream_headers_set"

type streamWriteError struct {
	err error
}

func (e *streamWriteError) Error() string {
	return e.err.Error()
}

func (e *streamWriteError) Unwrap() error {
	return e.err
}

func wrapStreamWriteError(err error) error {
	if err == nil {
		return nil
	}
	var writeErr *streamWriteError
	if errors.As(err, &writeErr) {
		return err
	}
	return &streamWriteError{err: err}
}

// IsStreamWriteError reports whether an error came from sending a stream
// frame to the downstream client rather than preparing the frame itself.
func IsStreamWriteError(err error) bool {
	var writeErr *streamWriteError
	return errors.As(err, &writeErr)
}

func FlushWriter(c *gin.Context) error {
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		return flushWriter(c)
	}))
}

func flushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	// Gin's response writer exposes Flush() without an error, but its wrapped
	// net/http writer may expose FlushError(). Walk the unwrap chain first so a
	// downstream write failure is not silently lost at the Gin boundary.
	c.Writer.WriteHeaderNow()
	writers := []http.ResponseWriter{c.Writer}
	current := http.ResponseWriter(c.Writer)
	for range 16 {
		unwrapper, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		next := unwrapper.Unwrap()
		if next == nil || next == current {
			break
		}
		writers = append(writers, next)
		current = next
	}
	for i := len(writers) - 1; i >= 0; i-- {
		if flusher, ok := writers[i].(interface{ FlushError() error }); ok {
			if err := flusher.FlushError(); err != nil {
				return fmt.Errorf("flush stream response failed: %w", err)
			}
			return nil
		}
	}
	for i := len(writers) - 1; i >= 0; i-- {
		if flusher, ok := writers[i].(http.Flusher); ok {
			flusher.Flush()
			return nil
		}
	}
	return errors.New("streaming error: flusher not found")
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	if c.GetBool(eventStreamHeadersSetKey) {
		return
	}

	c.Set(eventStreamHeadersSetKey, true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

// ResetEventStreamHeaders restores an uncommitted response so a handler can
// return a regular JSON error and a retry can prepare stream headers again.
func ResetEventStreamHeaders(c *gin.Context) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}

	header := c.Writer.Header()
	for name, value := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"Transfer-Encoding": "chunked",
		"X-Accel-Buffering": "no",
	} {
		if header.Get(name) == value {
			header.Del(name)
		}
	}
	c.Set(eventStreamHeadersSetKey, false)
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	jsonData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling stream response: %w", err)
	}
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		if requestContextDone(c) {
			return fmt.Errorf("request context done: %w", c.Request.Context().Err())
		}

		if err := (common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream event failed: %w", err)
		}
		if err := (common.CustomEvent{Data: "data: " + string(jsonData)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream data failed: %w", err)
		}
		return flushWriter(c)
	}))
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) error {
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		if requestContextDone(c) {
			return fmt.Errorf("request context done: %w", c.Request.Context().Err())
		}

		if err := (common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream event failed: %w", err)
		}
		if err := (common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream data failed: %w", err)
		}
		return flushWriter(c)
	}))
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		if requestContextDone(c) {
			return fmt.Errorf("request context done: %w", c.Request.Context().Err())
		}

		if err := (common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream event failed: %w", err)
		}
		if err := (common.CustomEvent{Data: fmt.Sprintf("data: %s", data)}).Render(c.Writer); err != nil {
			return fmt.Errorf("write stream data failed: %w", err)
		}
		return flushWriter(c)
	}))
}

func StringData(c *gin.Context, str string) error {
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		return stringData(c, str)
	}))
}

func stringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if err := (common.CustomEvent{Data: "data: " + str}).Render(c.Writer); err != nil {
		return fmt.Errorf("write stream data failed: %w", err)
	}
	return flushWriter(c)
}

func PingData(c *gin.Context) error {
	return wrapStreamWriteError(withStreamWrite(c, func() error {
		if c == nil || c.Writer == nil {
			return errors.New("context or writer is nil")
		}

		if requestContextDone(c) {
			return fmt.Errorf("request context done: %w", c.Request.Context().Err())
		}

		if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
			return fmt.Errorf("write ping data failed: %w", err)
		}
		return flushWriter(c)
	}))
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = withFinalStreamWrite(c, func() error {
		return stringData(c, "[DONE]")
	})
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
