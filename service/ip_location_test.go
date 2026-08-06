package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// formatGeoCoord 的 0 值语义是前端坐标行显隐的契约：0 视为缺失返回空，
// 真实坐标一律保留 5 位小数。
func TestFormatGeoCoord(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{name: "zero treated as missing", in: 0, want: ""},
		{name: "positive", in: 23.125178, want: "23.12518"},
		{name: "negative", in: -122.083847, want: "-122.08385"},
		{name: "whole number", in: 23, want: "23.00000"},
		{name: "tiny non-zero not truncated to empty", in: 0.00001, want: "0.00001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatGeoCoord(tt.in))
		})
	}
}
