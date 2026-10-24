package helper

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

// streamKeepAliveWriter 在业务写入前保活，写入开始时等待心跳退出。
type streamKeepAliveWriter struct {
	gin.ResponseWriter
	mutex       sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	pingContext *gin.Context
	abort       func()
	failure     error
}

func (writer *streamKeepAliveWriter) stop() {
	writer.cancel()
	<-writer.done
}

func (writer *streamKeepAliveWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *streamKeepAliveWriter) Write(data []byte) (int, error) {
	writer.stop()
	return writer.ResponseWriter.Write(data)
}

func (writer *streamKeepAliveWriter) WriteString(data string) (int, error) {
	writer.stop()
	return writer.ResponseWriter.WriteString(data)
}

func (writer *streamKeepAliveWriter) WriteHeader(status int) {
	writer.stop()
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *streamKeepAliveWriter) WriteHeaderNow() {
	writer.stop()
	writer.ResponseWriter.WriteHeaderNow()
}

func (writer *streamKeepAliveWriter) Flush() {
	writer.stop()
	writer.ResponseWriter.Flush()
}

func (writer *streamKeepAliveWriter) Status() int {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Status()
}

func (writer *streamKeepAliveWriter) Size() int {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Size()
}

func (writer *streamKeepAliveWriter) Written() bool {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.ResponseWriter.Written()
}

func (writer *streamKeepAliveWriter) ping() error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	ExtendWriteDeadline(writer.pingContext)
	err := PingData(writer.pingContext)
	if err != nil {
		writer.failure = err
		if writer.abort != nil {
			writer.abort()
		}
	}
	return err
}

// StartStreamKeepAlive 按 interval 发送心跳，覆盖请求准备和重试；业务写入开始后交给流处理器。
// 内层请求复用外层的保活，内层清理不会提前结束它。
func StartStreamKeepAlive(c *gin.Context, interval time.Duration, abort ...func()) func() {
	if previous, ok := c.Writer.(*streamKeepAliveWriter); ok {
		previous.mutex.Lock()
		if len(abort) > 0 {
			previous.abort = abort[0]
			if previous.failure != nil && previous.abort != nil {
				previous.abort()
			}
		}
		failed := previous.failure != nil
		previous.mutex.Unlock()
		if failed {
			return func() {}
		}
		select {
		case <-previous.done:
			c.Writer = previous.ResponseWriter
		default:
			return func() {}
		}
	}
	if requestContextDone(c) {
		return func() {}
	}
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	SetEventStreamHeaders(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Minute)
	pingContext := c.Copy()
	// 心跳使用原 writer，业务写入才会触发交接。
	pingContext.Writer = c.Writer
	writer := &streamKeepAliveWriter{ResponseWriter: c.Writer, cancel: cancel, done: make(chan struct{}), pingContext: pingContext}
	if len(abort) > 0 {
		writer.abort = abort[0]
	}
	c.Writer = writer
	go writer.keepAlive(ctx, interval)
	return writer.stop
}

func (writer *streamKeepAliveWriter) keepAlive(ctx context.Context, interval time.Duration) {
	defer close(writer.done)
	defer writer.cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.LogDebug(writer.pingContext, "SSE ping goroutine panic recovered: %v", recovered)
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil || writer.ping() != nil {
				return
			}
		}
	}
}

// StopStreamKeepAlive joins pre-response pings before response headers are read
// or changed by the successful upstream handler.
func StopStreamKeepAlive(c *gin.Context) {
	if writer, ok := c.Writer.(*streamKeepAliveWriter); ok {
		writer.stop()
	}
}
