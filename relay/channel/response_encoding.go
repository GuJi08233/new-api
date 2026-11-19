package channel

import (
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type decodedResponseBody struct {
	io.Reader
	closers []io.Closer
}

func (body *decodedResponseBody) Close() error {
	var err error
	for _, closer := range body.closers {
		err = errors.Join(err, closer.Close())
	}
	return err
}

// decodeUpstreamResponse keeps the client's Accept-Encoding intact while giving
// response handlers and usage accounting the decoded entity they expect.
func decodeUpstreamResponse(resp *http.Response) error {
	encoding := strings.Join(resp.Header.Values("Content-Encoding"), ",")
	if strings.TrimSpace(encoding) == "" || resp.Body == nil || resp.StatusCode == http.StatusNoContent ||
		resp.StatusCode == http.StatusNotModified || (resp.Request != nil && resp.Request.Method == http.MethodHead) {
		return nil
	}
	body := &decodedResponseBody{Reader: resp.Body, closers: []io.Closer{resp.Body}}
	resp.Body = body
	encodings := strings.Split(encoding, ",")
	for i := len(encodings) - 1; i >= 0; i-- {
		var reader io.ReadCloser
		var err error
		switch strings.ToLower(strings.TrimSpace(encodings[i])) {
		case "identity":
			continue
		case "gzip", "x-gzip":
			reader, err = gzip.NewReader(body.Reader)
		case "deflate":
			reader, err = zlib.NewReader(body.Reader)
		case "br":
			reader = io.NopCloser(brotli.NewReader(body.Reader))
		case "zstd":
			var decoder *zstd.Decoder
			decoder, err = zstd.NewReader(body.Reader, zstd.WithDecoderConcurrency(1))
			if err == nil {
				reader = decoder.IOReadCloser()
			}
		default:
			err = fmt.Errorf("unsupported content encoding %q", encodings[i])
		}
		if err != nil {
			return err
		}
		body.Reader = reader
		body.closers = append([]io.Closer{reader}, body.closers...)
	}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Uncompressed = true
	return nil
}
