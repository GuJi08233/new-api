package common

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingCustomEventWriter struct {
	header http.Header
	err    error
	failAt int
	calls  int
}

func (w *failingCustomEventWriter) Header() http.Header {
	return w.header
}

func (w *failingCustomEventWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

func (w *failingCustomEventWriter) WriteHeader(int) {}

func TestCustomEventRenderReturnsWriteError(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		writeErr := errors.New("downstream write failed")
		w := &failingCustomEventWriter{
			header: make(http.Header),
			err:    writeErr,
			failAt: failAt,
		}

		err := (CustomEvent{Data: "data: payload"}).Render(w)

		require.ErrorIs(t, err, writeErr)
	}
}
