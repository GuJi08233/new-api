package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// formatGeoCoord 的 nil/0 语义是前端坐标行显隐的契约：nil 表示上游缺失，
// 合法的 0 坐标与其他真实坐标一样保留 5 位小数。
func TestFormatGeoCoord(t *testing.T) {
	zero := 0.0
	positive := 23.125178
	negative := -122.083847
	whole := 23.0
	tiny := 0.00001
	tests := []struct {
		name string
		in   *float64
		want string
	}{
		{name: "missing", in: nil, want: ""},
		{name: "zero coordinate", in: &zero, want: "0.00000"},
		{name: "positive", in: &positive, want: "23.12518"},
		{name: "negative", in: &negative, want: "-122.08385"},
		{name: "whole number", in: &whole, want: "23.00000"},
		{name: "tiny non-zero", in: &tiny, want: "0.00001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatGeoCoord(tt.in))
		})
	}
}

func TestQueryGiteeIpLocationParsesLatLonAndPreservesZero(t *testing.T) {
	originalClient := ipLocationHTTPClient
	t.Cleanup(func() {
		ipLocationHTTPClient = originalClient
	})

	ipLocationHTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"ip":"1.1.1.1","continent":"测试洲","country":"测试国","lat":0,"lon":120.123456}`,
				)),
				Header: make(http.Header),
			}, nil
		}),
	}

	info, err := queryGiteeIpLocation("1.1.1.1", "test-key")
	require.NoError(t, err)
	assert.Equal(t, "测试国", info.Country)
	assert.Equal(t, "0.00000", info.Latitude)
	assert.Equal(t, "120.12346", info.Longitude)
}
