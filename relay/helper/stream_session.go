package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const streamSessionContextKey = "stream_write_session"

var errStreamSessionFinished = errors.New("stream session already finished")

type streamSession struct {
	writeMutex sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	stopOnce   sync.Once
	abortOnce  sync.Once
	abort      func()
	finished   bool
	failed     bool
	failure    error
}

// StartStreamSession prepares a provider-specific HTTP stream and starts its
// post-response keepalive. All helper SSE writes made before the returned stop
// function runs are serialized with ping writes through the Gin context.
func StartStreamSession(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) func() {
	return StartStreamSessionWithAbort(c, info, resp, nil)
}

// StartStreamSessionWithAbort additionally aborts provider transports that do
// not expose an HTTP response body (for example, AWS event streams and native
// WebSockets) when the downstream keepalive can no longer be written.
func StartStreamSessionWithAbort(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, abort func()) func() {
	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && (info == nil || !info.DisablePing)

	var (
		pingTicks <-chan time.Time
		stopTicks func()
	)
	if pingEnabled {
		pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
		if pingInterval <= 0 {
			pingInterval = DefaultPingInterval
		}
		ticker := time.NewTicker(pingInterval)
		pingTicks = ticker.C
		stopTicks = ticker.Stop
	}

	return startStreamSession(c, info, resp, pingTicks, stopTicks, nil, abort)
}

func startStreamSession(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	resp *http.Response,
	pingTicks <-chan time.Time,
	stopTicks func(),
	ping func() error,
	abort func(),
) func() {
	if c == nil || c.Writer == nil {
		if stopTicks != nil {
			stopTicks()
		}
		return func() {}
	}

	// doRequest normally copies these headers first. Repeating the guarded copy
	// here keeps the ordering invariant explicit for every custom stream entry.
	if info != nil && info.ChannelMeta != nil && info.ChannelSetting.PassThroughHeadersEnabled {
		CopyUpstreamResponseHeaders(c, resp)
	}
	SetEventStreamHeaders(c)

	if current := streamSessionFromContext(c); current != nil {
		if stopTicks != nil {
			stopTicks()
		}
		return func() {}
	}

	baseCtx := context.Background()
	if c.Request != nil {
		baseCtx = c.Request.Context()
	}
	pingerCtx, cancel := context.WithCancel(baseCtx)
	abortStream := func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.LogError(c, fmt.Sprintf("stream abort callback panic: %v", recovered))
			}
		}()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if abort != nil {
			abort()
		}
	}
	session := &streamSession{
		cancel: cancel,
		done:   make(chan struct{}),
		abort:  abortStream,
	}
	c.Set(streamSessionContextKey, session)

	if pingTicks == nil {
		if stopTicks != nil {
			stopTicks()
		}
		close(session.done)
	} else {
		if ping == nil {
			ping = func() error {
				return PingData(c)
			}
		}
		gopool.Go(func() {
			defer close(session.done)
			if stopTicks != nil {
				defer stopTicks()
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					err := fmt.Errorf("stream ping goroutine panic: %v", recovered)
					session.fail(err)
					logger.LogError(c, err.Error())
				}
			}()

			for {
				select {
				case <-pingTicks:
					if err := ping(); err != nil {
						if errors.Is(err, errStreamSessionFinished) {
							return
						}
						session.fail(err)
						logger.LogDebug(c, "stream ping stopped after write error: %s", err.Error())
						return
					}
				case <-pingerCtx.Done():
					return
				}
			}
		})
	}

	return func() {
		session.stopOnce.Do(func() {
			session.cancel()
			<-session.done
			session.writeMutex.Lock()
			failed := session.failed
			session.writeMutex.Unlock()
			if !failed {
				if current := streamSessionFromContext(c); current == session {
					c.Set(streamSessionContextKey, nil)
				}
			}
		})
	}
}

func (s *streamSession) fail(err error) {
	if s == nil || err == nil {
		return
	}
	s.writeMutex.Lock()
	s.markFailedLocked(err)
	s.writeMutex.Unlock()
	s.abortUpstream()
}

func (s *streamSession) markFailedLocked(err error) {
	if s == nil || err == nil || s.failed {
		return
	}
	s.failed = true
	s.failure = err
	s.finished = true
}

func (s *streamSession) abortUpstream() {
	if s == nil {
		return
	}
	s.abortOnce.Do(func() {
		if s.abort != nil {
			s.abort()
		}
	})
}

func (s *streamSession) writeError() error {
	if s == nil {
		return nil
	}
	if s.failure != nil {
		return fmt.Errorf("stream session failed: %w", s.failure)
	}
	if s.finished {
		return errStreamSessionFinished
	}
	return nil
}

func streamSessionFromContext(c *gin.Context) *streamSession {
	if c == nil {
		return nil
	}
	value, exists := c.Get(streamSessionContextKey)
	if !exists {
		return nil
	}
	session, _ := value.(*streamSession)
	return session
}

// StreamSessionWriteError returns the downstream write failure recorded by the
// active stream session. Providers that are reading a buffered upstream body
// can use it to distinguish an abort-triggered read error from an upstream
// response failure and still return usage for settlement.
func StreamSessionWriteError(c *gin.Context) error {
	session := streamSessionFromContext(c)
	if session == nil {
		return nil
	}
	session.writeMutex.Lock()
	defer session.writeMutex.Unlock()
	if !session.failed {
		return nil
	}
	return session.failure
}

func withStreamWrite(c *gin.Context, write func() error) error {
	if write == nil {
		return nil
	}
	session := streamSessionFromContext(c)
	if session == nil {
		ExtendWriteDeadline(c)
		return write()
	}

	session.writeMutex.Lock()
	if err := session.writeError(); err != nil {
		session.writeMutex.Unlock()
		return err
	}
	ExtendWriteDeadline(c)
	err := write()
	if err == nil {
		session.writeMutex.Unlock()
		return nil
	}
	session.markFailedLocked(err)
	session.writeMutex.Unlock()
	session.abortUpstream()
	return err
}

func withFinalStreamWrite(c *gin.Context, write func() error) error {
	if write == nil {
		return nil
	}
	session := streamSessionFromContext(c)
	if session == nil {
		ExtendWriteDeadline(c)
		return write()
	}

	session.writeMutex.Lock()
	if session.finished {
		session.writeMutex.Unlock()
		return nil
	}
	session.finished = true
	ExtendWriteDeadline(c)
	err := write()
	if err == nil {
		session.writeMutex.Unlock()
		return nil
	}
	session.markFailedLocked(err)
	session.writeMutex.Unlock()
	session.abortUpstream()
	return err
}
