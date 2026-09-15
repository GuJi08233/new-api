package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 渠道级自动禁用状态码契约：非空时替换全局状态码列表并视为该渠道显式开启自动禁用，
// 全局关键词与 channel: 类错误规则不变，跳过重试的错误永不禁用，无法解析的配置退回全局规则。
func TestShouldDisableChannelWithSetting(t *testing.T) {
	originalEnabled := common.AutomaticDisableChannelEnabled
	originalRanges := operation_setting.AutomaticDisableStatusCodeRanges
	originalKeywords := operation_setting.AutomaticDisableKeywords
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = originalEnabled
		operation_setting.AutomaticDisableStatusCodeRanges = originalRanges
		operation_setting.AutomaticDisableKeywords = originalKeywords
	})
	require.NoError(t, operation_setting.AutomaticDisableStatusCodesFromString("401"))
	operation_setting.AutomaticDisableKeywords = []string{"your credit balance is too low"}

	upstream := func(statusCode int, ops ...types.NewAPIErrorOptions) *types.NewAPIError {
		return types.NewErrorWithStatusCode(errors.New("upstream error"), types.ErrorCodeBadResponse, statusCode, ops...)
	}
	channelCodes := dto.ChannelSettings{AutoDisableStatusCodes: "429"}

	tests := []struct {
		name          string
		globalEnabled bool
		setting       dto.ChannelSettings
		err           *types.NewAPIError
		want          bool
	}{
		{name: "nil error never disables", globalEnabled: true, setting: channelCodes, err: nil, want: false},
		{name: "global off without channel codes never disables", globalEnabled: false, err: upstream(http.StatusUnauthorized), want: false},
		{name: "global on follows global codes", globalEnabled: true, err: upstream(http.StatusUnauthorized), want: true},
		{name: "global on ignores codes outside the global list", globalEnabled: true, err: upstream(http.StatusTooManyRequests), want: false},
		{name: "channel codes replace the global list", globalEnabled: true, setting: channelCodes, err: upstream(http.StatusUnauthorized), want: false},
		{name: "channel codes match", globalEnabled: true, setting: channelCodes, err: upstream(http.StatusTooManyRequests), want: true},
		{name: "channel codes work even when global is off", globalEnabled: false, setting: dto.ChannelSettings{AutoDisableStatusCodes: "401,429"}, err: upstream(http.StatusTooManyRequests), want: true},
		{name: "channel codes miss with global off", globalEnabled: false, setting: channelCodes, err: upstream(http.StatusInternalServerError), want: false},
		{name: "skip retry error never disables", globalEnabled: false, setting: channelCodes, err: upstream(http.StatusTooManyRequests, types.ErrOptionWithSkipRetry()), want: false},
		{name: "channel config error disables once the channel opted in", globalEnabled: false, setting: channelCodes, err: types.NewError(errors.New("bad key"), types.ErrorCodeChannelInvalidKey), want: true},
		{name: "global keywords still apply once the channel opted in", globalEnabled: false, setting: channelCodes, err: types.NewErrorWithStatusCode(errors.New("Your credit balance is too low"), types.ErrorCodeBadResponse, http.StatusBadRequest), want: true},
		{name: "unparsable channel codes fall back to global rules", globalEnabled: false, setting: dto.ChannelSettings{AutoDisableStatusCodes: "4xx"}, err: upstream(http.StatusUnauthorized), want: false},
		{name: "blank channel codes follow global rules", globalEnabled: true, setting: dto.ChannelSettings{AutoDisableStatusCodes: "  "}, err: upstream(http.StatusUnauthorized), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			common.AutomaticDisableChannelEnabled = tc.globalEnabled
			assert.Equal(t, tc.want, ShouldDisableChannelWithSetting(tc.err, tc.setting))
		})
	}
}
