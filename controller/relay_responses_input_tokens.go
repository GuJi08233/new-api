package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// RelayResponsesInputTokens proxies POST /v1/responses/input_tokens to the
// selected channel without charging generation quota.
func RelayResponsesInputTokens(c *gin.Context) {
	requestId := c.GetString(common.RequestIdKey)
	var newAPIError *types.NewAPIError

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay responses input_tokens error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(service.MaskUpstreamModelInText(c, newAPIError.Error()), requestId))
			c.JSON(newAPIError.StatusCode, gin.H{
				"error": newAPIError.ToOpenAIError(),
			})
		}
	}()

	req, err := helper.GetAndValidateResponsesRequest(c)
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIResponses, req, nil)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	info.RelayMode = relayconstant.RelayModeResponsesInputTokens
	info.IsStream = false

	if endpointErr := checkModelEndpointProtection(c, info.OriginModelName, c.Request.URL.Path); endpointErr != nil {
		newAPIError = endpointErr
		return
	}

	info.InitChannelMeta(c)
	copied, err := common.DeepCopy(req)
	if err != nil {
		newAPIError = types.NewError(fmt.Errorf("failed to copy responses input_tokens request: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		return
	}
	if err := helper.ModelMappedHelper(c, info, copied); err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		return
	}

	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		newAPIError = types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
		return
	}
	adaptor.Init(info)

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		passBody, err := relaycommon.GetPassThroughRequestBody(c, info)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
			return
		}
		requestBody = passBody
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *copied)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			return
		}
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			return
		}
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			return
		}
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeChannelParamOverrideInvalid, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				return
			}
		}
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			return
		}
		defer closer.Close()
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	finish, admissionErr := service.BeginChannelAttempt(c, info.ChannelId, info.ChannelSetting)
	if admissionErr != nil {
		newAPIError = admissionErr
		return
	}
	defer finish(false)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		newAPIError = types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		return
	}

	httpResp := resp.(*http.Response)
	statusCodeMappingStr := c.GetString("status_code_mapping")
	if httpResp.StatusCode != http.StatusOK {
		newAPIError = service.RelayErrorHandler(c, httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return
	}

	defer service.CloseResponseBodyGracefully(httpResp)
	responseBody, err := service.ReadResponseBody(c, httpResp)
	if err != nil {
		newAPIError = types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		return
	}
	finish(true)
	service.IOCopyRawBytesGracefully(c, httpResp, responseBody)
}
