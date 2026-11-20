package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func BeginChannelAttempt(c *gin.Context, channelID int, settings dto.ChannelSettings) (func(bool), *types.NewAPIError) {
	finish, err := model.BeginChannelAttempt(c.Request.Context(), channelID, model.NewChannelRequestLimitConfig(settings))
	if err == nil {
		return finish, nil
	}
	if errors.Is(err, model.ErrChannelRequestLimited) {
		return nil, ChannelRequestLimitedError(c)
	}
	if c.Request.Context().Err() != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelAdmissionFailed, http.StatusRequestTimeout,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	common.SysError(fmt.Sprintf("channel #%d admission failed: %v", channelID, err))
	return nil, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorChannelAdmissionFailed)), types.ErrorCodeChannelAdmissionFailed,
		http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func ChannelRequestLimitedError(c *gin.Context) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorChannelRequestLimited)), types.ErrorCodeChannelRequestLimited,
		http.StatusTooManyRequests, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

// SkipLimitedChannel excludes a locally rejected attempt from subsequent
// selection without spending the upstream retry budget.
func SkipLimitedChannel(c *gin.Context, channelID int) bool {
	if c.Request.Context().Err() != nil {
		return false
	}
	if _, fixed := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); fixed {
		return false
	}
	constraints := GetChannelConstraints(c)
	if len(constraints.Pins) != 0 {
		return false
	}
	for _, filter := range constraints.Filters {
		if filter.Kind == dto.FilterLimitedChannel && filter.ChannelID == channelID {
			return false
		}
	}
	constraints.AddFilter(dto.ChannelFilter{Kind: dto.FilterLimitedChannel, ChannelID: channelID})
	// A group may have advanced its cursor in anticipation of a real upstream
	// failure. Local rejection should still consider that group's other channels.
	group := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	if group != "" {
		for i, candidate := range ResolveCandidateGroups(c, common.GetContextKeyString(c, constant.ContextKeyTokenGroup)) {
			if candidate == group {
				common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, i)
				break
			}
		}
	}
	return true
}
