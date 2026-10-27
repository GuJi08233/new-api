package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if isGeminiEmbeddingRequestPath(c.Request.URL.Path) {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func isGeminiEmbeddingRequestPath(path string) bool {
	return strings.Contains(path, ":embedContent") || strings.Contains(path, ":batchEmbedContents")
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
		relayInfo   *relaycommon.RelayInfo
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			// 后端日志与错误日志（processChannelError）都已在此之前落库，保留上游原文；
			// 只有发给客户端的这一份把上游模型名换回用户请求的模型名。
			newAPIError.SetMessage(common.MessageWithRequestId(service.MaskUpstreamModelInText(c, newAPIError.Error()), requestId))
			if relayFormat != types.RelayFormatOpenAIRealtime && c.Writer.Written() &&
				strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
				var status *relaycommon.StreamStatus
				if relayInfo != nil {
					status = relayInfo.StreamStatus
				}
				if status == nil {
					status = relaycommon.NewStreamStatus()
					if relayInfo != nil {
						relayInfo.StreamStatus = status
					}
				}
				status.RecordError(newAPIError.Error())
				status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, newAPIError)
				if relayFormat == types.RelayFormatClaude {
					_ = helper.ClaudeData(c, dto.ClaudeResponse{Type: "error", Error: newAPIError.ToClaudeError()})
				} else {
					_ = helper.ObjectData(c, gin.H{"error": newAPIError.ToOpenAIError()})
					helper.Done(c)
				}
				return
			}
			if relayFormat != types.RelayFormatOpenAIRealtime {
				helper.ResetEventStreamHeaders(c)
			}
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	if documentCount, known := dto.GetRequestDocumentCount(request); known {
		common.SetContextKey(c, constant.ContextKeyEstimatedDocs, documentCount)
	}

	// 模型端点保护校验
	if endpointErr := checkModelEndpointProtection(c, relayInfo.OriginModelName, c.Request.URL.Path); endpointErr != nil {
		newAPIError = endpointErr
		return
	}
	generalSettings := operation_setting.GetGeneralSetting()
	initialChannelSetting, _ := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if relayFormat == types.RelayFormatClaude && relayInfo.IsStream && generalSettings.PingIntervalEnabled && !initialChannelSetting.PassThroughHeadersEnabled {
		stopKeepAlive := helper.StartStreamKeepAlive(c, time.Duration(generalSettings.PingIntervalSeconds)*time.Second)
		defer stopKeepAlive()
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)
	// Zero is a valid estimate, but only when token counting actually ran.
	// Otherwise token-based tiers must remain unknown instead of matching zero.
	if constant.CountToken && relayInfo.RelayFormat != types.RelayFormatOpenAIRealtime {
		common.SetContextKey(c, constant.ContextKeyEstimatedTokens, tokens)
	} else if c.Keys != nil {
		delete(c.Keys, string(constant.ContextKeyEstimatedTokens))
	}

	// Re-route after estimating tokens. During the Distribute middleware phase,
	// estimatedTokens is 0 so tier routing cannot evaluate and only the Fallback
	// Pool is used. Now that real tokens are known, evaluate the route again so
	// the precise tier is picked. The first getChannel() in the retry loop will
	// short-circuit on info.ChannelMeta==nil and return whatever channel_id sits
	// in the context, so updating the context here is enough.
	needsReroute := common.GetContextKeyBool(c, constant.ContextKeyChannelRouteNeedsReroute)
	channelBeforeReroute := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if reRouteErr := rerouteByRequestMetrics(c, relayInfo); reRouteErr != nil {
		newAPIError = reRouteErr
		return
	}
	// Distribute 对待重路由的占位渠道跳过了绑定渠道的客户端 UA 规则，重路由后对最终渠道补判
	if channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId); needsReroute || channelID != channelBeforeReroute {
		if ruleErr := service.RejectByClientUaRule(c, relayInfo.OriginModelName, channelID); ruleErr != nil {
			newAPIError = ruleErr
			return
		}
	}

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)
	if _, isImage := request.(*dto.ImageRequest); isImage {
		relayInfo.ForcePreConsume = true
	}

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for retryParam.GetRetry() <= common.RetryTimes {
		// 客户端已断开时不再换渠道重试；LastError 非空保证首次尝试不受影响
		if relayInfo.LastError != nil && c.Request.Context().Err() != nil {
			break
		}
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}

		addUsedChannel(c, channel.Id)
		if billingErr := helper.PrepareBillingForSelectedGroup(c, relayInfo, tokens, meta); billingErr != nil {
			newAPIError = billingErr
			break
		}

		channelSetting := getChannelRetrySettings(c, channel)
		effectiveRetryTimes := common.RetryTimes
		if channelSetting.RetryTimes > 0 {
			effectiveRetryTimes = channelSetting.RetryTimes
		}
		// When retry_on_same_channel is enabled with explicit retry_times > 0,
		// same-channel retries are independent of the global RetryTimes budget.
		independentSameChannelRetry := channelSetting.RetryOnSameChannel && channelSetting.RetryTimes > 0

		sameChannelRetry := 0
		for {
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				newAPIError = relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				newAPIError = relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				newAPIError = geminiRelayHandler(c, relayInfo)
			case types.RelayFormatTypeSafe:
				newAPIError = relay.TypeSafeHelper(c, relayInfo)
			default:
				newAPIError = relayHandler(c, relayInfo)
			}

			if newAPIError == nil {
				relayInfo.LastError = nil
				// 渠道级请求频率限制的成功计数：只有在该渠道上成功完成的调用才计入
				model.IncrChannelSuccessCount(channel.Id, model.NewChannelRequestLimitConfig(channelSetting))
				return
			}

			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			relayInfo.LastError = newAPIError

			processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

			// Determine remaining retry budget for shouldRetry check
			var remainingForCheck int
			if independentSameChannelRetry {
				remainingForCheck = effectiveRetryTimes - sameChannelRetry
			} else {
				remainingForCheck = common.RetryTimes - retryParam.GetRetry()
			}
			if !shouldRetryWithChannelSetting(c, newAPIError, remainingForCheck, channelSetting) {
				break
			}

			// Same-channel retry logic
			if channelSetting.RetryOnSameChannel && sameChannelRetry < effectiveRetryTimes {
				if independentSameChannelRetry {
					// Independent mode: don't consume global retry budget
					sameChannelRetry++
					relayInfo.RetryIndex++
				} else if retryParam.GetRetry() < common.RetryTimes {
					// Legacy mode: consume global retry budget
					sameChannelRetry++
					retryParam.IncreaseRetry()
					relayInfo.RetryIndex = retryParam.GetRetry()
				} else {
					break
				}
				// Re-setup context for same channel (supports multi-key rotation)
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					newAPIError = setupErr
					break
				}
				continue
			}
			break
		}

		// After inner loop: check if we should transfer to another channel or fail
		if newAPIError == nil {
			return
		}
		if channelSetting.RetryOnSameChannel && channelSetting.RetryExhaustedTransfer != nil && !*channelSetting.RetryExhaustedTransfer {
			// Configured to fail directly after same-channel retries exhausted
			break
		}
		if independentSameChannelRetry {
			// Independent mode exhausted: if transfer allowed, try next channel with global budget
			if retryParam.GetRetry() >= common.RetryTimes {
				break
			}
			retryParam.IncreaseRetry()
			continue
		}
		if !shouldRetryWithChannelSetting(c, newAPIError, common.RetryTimes-retryParam.GetRetry(), channelSetting) {
			break
		}
		if retryParam.GetRetry() >= common.RetryTimes {
			break
		}
		retryParam.IncreaseRetry()
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		if setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok && setting.RetryOnSameChannel {
			channel, err := model.CacheGetChannel(channelId)
			if err != nil {
				return nil, types.NewError(fmt.Errorf("重新加载同渠道重试渠道 #%d 失败: %w", channelId, err), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
			return channel, nil
		}
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      channelId,
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	restoreAutoRouteContext := snapshotAutoRouteContext(c)
	routeRetryParam := &service.RetryParam{
		Ctx:         retryParam.Ctx,
		TokenGroup:  retryParam.TokenGroup,
		ModelName:   retryParam.ModelName,
		RequestPath: retryParam.RequestPath,
		Retry:       common.GetPointer(retryParam.GetRetry()),
	}
	routeMatch, routeErr := service.GetChannelByRoute(routeRetryParam)
	if routeErr != nil {
		restoreAutoRouteContext()
		return nil, types.NewError(fmt.Errorf("获取模型 %s 的静态渠道路由失败: %s", info.OriginModelName, routeErr.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if routeMatch.Matched {
		if routeMatch.Rejected {
			restoreAutoRouteContext()
			return nil, newChannelRouteRejectedError(c, info.OriginModelName, routeMatch)
		}
		if routeMatch.Channel != nil {
			if ruleErr := clientUaRuleRetryError(c, info, routeMatch.Channel.Id); ruleErr != nil {
				restoreAutoRouteContext()
				return nil, ruleErr
			}
			if service.IsMultiCandidateGroup(retryParam.TokenGroup) && routeMatch.SelectGroup != "" {
				common.SetContextKey(c, constant.ContextKeyAutoGroup, routeMatch.SelectGroup)
			}
			newAPIError := middleware.SetupContextForSelectedChannel(c, routeMatch.Channel, info.OriginModelName)
			if newAPIError != nil {
				restoreAutoRouteContext()
				return nil, newAPIError
			}
			info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
			return routeMatch.Channel, nil
		}
		if routeMatch.Strict && routeMatch.Exhausted && !routeMatch.Deferred {
			restoreAutoRouteContext()
			return nil, types.NewError(fmt.Errorf("静态渠道路由规则 %s 命中，但未找到可用渠道", routeMatch.RuleName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		restoreAutoRouteContext()
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if ruleErr := clientUaRuleRetryError(c, info, channel.Id); ruleErr != nil {
		return nil, ruleErr
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
	return channel, nil
}

// clientUaRuleRetryError 在重试选中受客户端 UA 规则限制的渠道时结束重试。
// 返回上一次的上游错误，让客户端看到原始失败原因而不是规则拒绝；
// 只有尚无上游错误时才返回规则拒绝本身。渠道未受限时返回 nil。
func clientUaRuleRetryError(c *gin.Context, info *relaycommon.RelayInfo, channelID int) *types.NewAPIError {
	if service.MatchClientUaRule(c, info.OriginModelName, channelID) == nil {
		return nil
	}
	if info.LastError != nil {
		return info.LastError
	}
	return service.RejectByClientUaRule(c, info.OriginModelName, channelID)
}

// snapshotAutoRouteContext saves the auto-group routing cursor keys and returns
// a function that restores them. GetChannelByRoute advances these keys while it
// scans auto groups, so any caller that ends up not using its result must
// restore them; otherwise later channel selection starts from a polluted
// cursor and may skip groups that still have usable channels.
func snapshotAutoRouteContext(c *gin.Context) func() {
	if c == nil {
		return func() {}
	}
	savedAutoGroup, savedAutoGroupExists := common.GetContextKey(c, constant.ContextKeyAutoGroup)
	savedAutoGroupIndex, savedAutoGroupIndexExists := common.GetContextKey(c, constant.ContextKeyAutoGroupIndex)
	savedAutoGroupRetryIndex, savedAutoGroupRetryIndexExists := common.GetContextKey(c, constant.ContextKeyAutoGroupRetryIndex)
	return func() {
		if c == nil || c.Keys == nil {
			return
		}
		if savedAutoGroupExists {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, savedAutoGroup)
		} else {
			delete(c.Keys, string(constant.ContextKeyAutoGroup))
		}
		if savedAutoGroupIndexExists {
			common.SetContextKey(c, constant.ContextKeyAutoGroupIndex, savedAutoGroupIndex)
		} else {
			delete(c.Keys, string(constant.ContextKeyAutoGroupIndex))
		}
		if savedAutoGroupRetryIndexExists {
			common.SetContextKey(c, constant.ContextKeyAutoGroupRetryIndex, savedAutoGroupRetryIndex)
		} else {
			delete(c.Keys, string(constant.ContextKeyAutoGroupRetryIndex))
		}
	}
}

// rerouteByRequestMetrics finalizes a provisional channel route after validated
// document counts and token estimates are available. Explicit channel selection
// remains pinned, and unused auto-group cursor mutations are restored.
func rerouteByRequestMetrics(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if _, specified := c.Get("specific_channel_id"); specified {
		return nil
	}
	if _, exists := common.GetContextKey(c, constant.ContextKeyChannelRouteNeedsReroute); exists &&
		!common.GetContextKeyBool(c, constant.ContextKeyChannelRouteNeedsReroute) {
		return nil
	}
	if c.Keys != nil {
		defer delete(c.Keys, string(constant.ContextKeyChannelRouteNeedsReroute))
	}
	restoreAutoRouteContext := snapshotAutoRouteContext(c)
	routeMatch, err := service.GetChannelByRoute(&service.RetryParam{
		Ctx:         c,
		TokenGroup:  info.TokenGroup,
		ModelName:   info.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	})
	if err != nil {
		restoreAutoRouteContext()
		return types.NewError(
			fmt.Errorf("重新路由失败: %s", err.Error()),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if !routeMatch.Matched {
		restoreAutoRouteContext()
		return nil
	}
	if routeMatch.Rejected {
		restoreAutoRouteContext()
		return newChannelRouteRejectedError(c, info.OriginModelName, routeMatch)
	}
	if routeMatch.Channel == nil {
		if routeMatch.Strict && routeMatch.Exhausted && !routeMatch.Deferred {
			restoreAutoRouteContext()
			return types.NewError(
				fmt.Errorf("静态渠道路由规则 %s 命中，但未找到可用渠道", routeMatch.RuleName),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			)
		}
		restoreAutoRouteContext()
		if !routeMatch.Tiered {
			return nil
		}
		return rerouteFallbackToNormalSelection(c, info)
	}
	if routeMatch.Channel.Id == common.GetContextKeyInt(c, constant.ContextKeyChannelId) {
		restoreAutoRouteContext()
		return nil
	}
	if apiErr := middleware.SetupContextForSelectedChannel(c, routeMatch.Channel, info.OriginModelName); apiErr != nil {
		restoreAutoRouteContext()
		return apiErr
	}
	if service.IsMultiCandidateGroup(info.TokenGroup) && routeMatch.SelectGroup != "" {
		common.SetContextKey(c, constant.ContextKeyAutoGroup, routeMatch.SelectGroup)
	}
	return nil
}

func newChannelRouteRejectedError(c *gin.Context, modelName string, routeMatch *service.ChannelRouteMatch) *types.NewAPIError {
	rejectMessage := strings.TrimSpace(routeMatch.RejectMessage)
	if rejectMessage == "" {
		rejectMessage = i18n.T(c, i18n.MsgDistributorRuleRejected, map[string]any{"Rule": routeMatch.RuleName, "Model": modelName})
	}
	return types.NewErrorWithStatusCode(
		errors.New(rejectMessage),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

// rerouteFallbackToNormalSelection replaces the placeholder channel chosen by
// Distribute with one from normal (non-route) selection, mirroring the
// non-strict fallback in the Distribute middleware.
func rerouteFallbackToNormalSelection(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
		Ctx:         c,
		TokenGroup:  info.TokenGroup,
		ModelName:   info.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	})
	if err != nil {
		return types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel.Id == common.GetContextKeyInt(c, constant.ContextKeyChannelId) {
		return nil
	}
	return middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	// 客户端已断开，重试请求携带的 context 已取消，发出去也会立即失败
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

// getChannelRetrySettings retrieves ChannelSettings from gin context (preferred)
// or from the channel object. On the first relay iteration the channel returned by
// getChannel is a minimal object without Setting populated, so context is the
// authoritative source (set by Distribute middleware or SetupContextForSelectedChannel).
func getChannelRetrySettings(c *gin.Context, channel *model.Channel) dto.ChannelSettings {
	if setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
		return setting
	}
	return channel.GetSetting()
}

// shouldRetryWithChannelSetting wraps shouldRetry with per-channel retry status
// code override. When the channel has configured RetryStatusCodes, those codes
// replace the global AutomaticRetryStatusCodes for the status-code check.
func shouldRetryWithChannelSetting(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int, setting dto.ChannelSettings) bool {
	if openaiErr == nil {
		return false
	}
	// 客户端已断开，重试请求携带的 context 已取消，发出去也会立即失败
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	// Per-channel retry status codes override global
	if setting.RetryStatusCodes != "" {
		ranges, err := operation_setting.ParseHTTPStatusCodeRanges(setting.RetryStatusCodes)
		if err == nil && len(ranges) > 0 {
			return operation_setting.MatchStatusCodeRanges(ranges, code)
		}
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	// 渠道级自动禁用状态码取自本次请求命中渠道的上下文：判定在此同步完成，只有禁用动作异步执行。
	channelSetting, _ := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if service.ShouldDisableChannelWithSetting(err, channelSetting) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		// 错误日志与消费日志用同一口径：auto/多分组令牌记录本次实际命中的分组
		userGroup := service.ResolveEffectiveGroup(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		if rewrite, ok := service.GetModelRewrite(c); ok {
			// 记录本次实际请求的上游模型，管理员据此排错；用户侧查询时
			// formatUserLogs 会用它把正文里的上游模型名替换掉再删除该字段。
			// 两个前端都要求 is_model_mapped 与 upstream_model_name 同时存在
			// 才渲染「实际模型」，所以标记必须一起写。
			// 取归一化后的实时上游名，与消费日志里的 relayInfo.UpstreamModelName 一致。
			other["is_model_mapped"] = true
			other["upstream_model_name"] = rewrite.UpstreamModelName()
		}
		adminInfo := make(map[string]interface{})
		adminInfo["channel_id"] = channelId
		adminInfo["channel_name"] = c.GetString("channel_name")
		adminInfo["channel_type"] = c.GetInt("channel_type")
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		elapsedMilliseconds := max(time.Since(startTime).Milliseconds(), 0)
		other["use_time_ms"] = elapsedMilliseconds
		useTimeSeconds := int(elapsedMilliseconds / 1000)
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

// RelayTaskPluginEndpoint keeps unclaimed shared-endpoint traffic on its
// existing handler while claimed requests enter the generation-pinned
// host-owned protocol bridge.
func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Task protocol request failed",
				"type":    "new_api_error",
				"code":    "task_protocol_error",
			},
		})
		return
	}
	switch pinned.Protocol {
	case "openai_responses":
		serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	case pluginruntime.ProtocolOpenAIImage:
		serveTaskPluginImageProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	default:
		fallback(c)
	}
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskSubmissionError(c, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if action := c.GetString("task_action"); action != "" {
		relayInfo.Action = action
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	if taskErr := relay.ApplyOriginTaskAffinity(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	// 续作任务的模型与渠道取自原任务，Distribute 入口处还不知道，在锁定原渠道后补做客户端 UA 判定
	if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
		for _, channelID := range []int{0, lockedCh.Id} {
			if ruleErr := service.RejectByClientUaRule(c, relayInfo.OriginModelName, channelID); ruleErr != nil {
				respondTaskSubmissionError(c, service.TaskErrorFromAPIError(ruleErr))
				return
			}
		}
	}

	outcome, taskErr := executeTaskSubmission(c, relayInfo)
	if taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	presentTaskSubmission(c, outcome)
}

// executeTaskSubmission owns the retry, billing, and persistence lifecycle.
// It deliberately performs no client response writes so JSON and protocol
// presenters share the same durable task barrier. Its cancellation semantics
// come from c.Request.Context: native task endpoints use the client context,
// while the Responses bridge supplies an independently bounded context.
func executeTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
	return executeTaskSubmissionWith(c, relayInfo, relay.RelayTaskSubmit)
}

type taskSubmitAttempt func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError)

func executeTaskSubmissionWith(
	c *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	submit taskSubmitAttempt,
) (*taskSubmissionOutcome, *dto.TaskError) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	diagnostics.start(relayInfo)
	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	durable := false
	stage := "start"
	defer func() {
		if !durable && relayInfo.Billing != nil {
			diagnostics.refund(stage)
			relayInfo.Billing.Refund(c)
		}
	}()
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		diagnostics.cancelled("before_attempt", 0)
		return nil, service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
	}

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}

	attempt := 0
	for retryParam.GetRetry() <= common.RetryTimes {
		stage = "select_channel"
		// 客户端已断开时不再换渠道重试
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			diagnostics.cancelled("before_attempt", attempt+1)
			taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
			break
		}
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				// 重试命中客户端 UA 规则时 getChannel 原样返回上一次的上游错误，保留原 taskErr
				if relayInfo.LastError != nil && channelErr == relayInfo.LastError {
					break
				}
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", channelErr.StatusCode)
				break
			}
		}

		addUsedChannel(c, channel.Id)

		channelSetting := getChannelRetrySettings(c, channel)
		effectiveRetryTimes := common.RetryTimes
		if channelSetting.RetryTimes > 0 {
			effectiveRetryTimes = channelSetting.RetryTimes
		}
		independentSameChannelRetry := channelSetting.RetryOnSameChannel && channelSetting.RetryTimes > 0

		sameChannelRetry := 0
		for {
			attempt++
			diagnostics.attempt(attempt, channel, relayInfo.LockedChannel != nil)
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				stage = "read_body"
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
				} else {
					taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
				}
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			stage = "submit"
			result, taskErr = submit(c, relayInfo)
			if taskErr == nil {
				diagnostics.attemptSucceeded(attempt, result)
				model.IncrChannelSuccessCount(channel.Id, model.NewChannelRequestLimitConfig(channelSetting))
				break
			}
			if requestErr := c.Request.Context().Err(); requestErr != nil {
				diagnostics.cancelled("after_submit", attempt)
				taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
				break
			}
			relayInfo.LastError = taskSubmissionAPIError(taskErr)

			if !taskErr.LocalError {
				processChannelError(c,
					*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
						common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
					relayInfo.LastError)
			}

			var remainingForCheck int
			if independentSameChannelRetry {
				remainingForCheck = effectiveRetryTimes - sameChannelRetry
			} else {
				remainingForCheck = common.RetryTimes - retryParam.GetRetry()
			}
			willRetry := shouldRetryTaskRelay(c, channel.Id, taskErr, remainingForCheck)
			diagnostics.attemptFailed(attempt, channel, taskErr, willRetry)
			if !willRetry {
				break
			}

			// Same-channel retry logic for task relay
			if channelSetting.RetryOnSameChannel && sameChannelRetry < effectiveRetryTimes {
				if independentSameChannelRetry {
					sameChannelRetry++
				} else if retryParam.GetRetry() < common.RetryTimes {
					sameChannelRetry++
					retryParam.IncreaseRetry()
				} else {
					break
				}
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
				continue
			}
			break
		}

		// After inner loop: check if we should transfer or fail
		if taskErr == nil {
			break
		}
		if stage == "read_body" || taskErr.NoRetry {
			break
		}
		if channelSetting.RetryOnSameChannel && channelSetting.RetryExhaustedTransfer != nil && !*channelSetting.RetryExhaustedTransfer {
			break
		}
		if independentSameChannelRetry {
			if retryParam.GetRetry() >= common.RetryTimes {
				break
			}
			retryParam.IncreaseRetry()
			continue
		}
		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
		if retryParam.GetRetry() >= common.RetryTimes {
			break
		}
		retryParam.IncreaseRetry()
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	if taskErr != nil {
		diagnostics.failed(stage, "task_error", taskErr, false)
		return nil, taskErr
	}
	if result == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError)
		diagnostics.failed("submit", "missing_result", taskErr, false)
		return nil, taskErr
	}

	// Reserve any submit-time upward billing adjustment before persistence.
	// This keeps insertion failures fully refundable while ensuring settlement
	// after the barrier normally has a zero positive delta.
	if relayInfo.Billing != nil {
		stage = "reserve"
		diagnostics.reserve("reserve_start", result.Quota)
		if reserveErr := relayInfo.Billing.Reserve(result.Quota); reserveErr != nil {
			common.SysError("reserve adjusted task billing error: " + reserveErr.Error())
			taskErr = service.TaskErrorWrapperLocal(errors.New("insufficient quota for adjusted task cost"), string(types.ErrorCodeInsufficientUserQuota), http.StatusForbidden)
			diagnostics.failed("reserve", "insufficient_quota", taskErr, false)
			return nil, taskErr
		}
		diagnostics.reserve("reserve_complete", result.Quota)
	}

	// 上游已经接受任务：从这里起客户端断开也必须落库，否则任务丢失却已扣费。
	stage = "insert"
	task := model.InitTask(result.Platform, relayInfo)
	task.PrivateData.Execution = service.TaskExecutionSnapshotFromContext(c)
	task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
	task.PrivateData.BillingSource = relayInfo.BillingSource
	task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.Source = model.ClientLogSource(c)
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      relayInfo.PriceData.ModelPrice,
		GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelRatio:      relayInfo.PriceData.ModelRatio,
		OtherRatios:     relayInfo.PriceData.OtherRatios(),
		OriginModelName: relayInfo.OriginModelName,
		PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		TieredSnapshot:  relayInfo.TieredBillingSnapshot,
	}
	task.Quota = result.Quota
	task.Data = result.TaskData
	if len(result.PluginState) > 0 {
		task.PrivateData.PluginState = result.PluginState
	}
	task.Action = relayInfo.Action
	if immediate := result.Immediate; immediate != nil {
		task.Status = model.TaskStatus(immediate.Status)
		task.Progress = immediate.Progress
		if immediate.Status == model.TaskStatusSuccess || immediate.Status == model.TaskStatusFailure {
			task.FinishTime = time.Now().Unix()
		}
		if immediate.Status == model.TaskStatusFailure {
			task.FailReason = immediate.Reason
		}
		if immediate.Url != "" {
			task.PrivateData.ResultURL = immediate.Url
		} else if immediate.Status == model.TaskStatusSuccess {
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	}
	// A native submit route may declare retainResult: false. It applies only
	// to immediate terminal results: the client receives the complete response
	// once, the upstream snapshot is never persisted, and the task is not
	// retrievable afterwards. An asynchronous result on such a route keeps its
	// snapshot because polling and retrieval need it. The OpenAI Images
	// protocol delivers its images inline in the same HTTP response, so its
	// immediate results follow the same rule.
	var insertOmits []string
	immediateTerminal := result.Immediate != nil && (result.Immediate.Status == model.TaskStatusSuccess || result.Immediate.Status == model.TaskStatusFailure)
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
		pinned, ok := pinnedValue.(pluginruntime.PinnedRoute)
		if ok && pinned.Route.RetainResult != nil && !*pinned.Route.RetainResult {
			if immediateTerminal {
				task.PrivateData.ResultDiscarded = true
				insertOmits = append(insertOmits, "data")
			} else {
				logger.LogWarn(c, fmt.Sprintf("task plugin route %s %s declares retainResult: false but returned an asynchronous result; retaining task %s", pinned.Route.Method, pinned.Route.Path, task.TaskID))
			}
		}
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists && immediateTerminal {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint); ok && pinned.Protocol == pluginruntime.ProtocolOpenAIImage {
			task.PrivateData.ResultDiscarded = true
			insertOmits = append(insertOmits, "data")
		}
	}
	diagnostics.insertStart(task)
	if insertErr := task.InsertWithContext(context.WithoutCancel(c.Request.Context()), insertOmits...); insertErr != nil {
		common.SysError("insert task error: " + insertErr.Error())
		taskErr = service.TaskErrorWrapperLocal(errors.New("failed to persist task"), "task_insert_failed", http.StatusInternalServerError)
		diagnostics.failed("insert", "database_error", taskErr, false)
		return nil, taskErr
	}
	durable = true
	stage = "settle"
	diagnostics.durable(task)
	diagnostics.settleStart(task, result.Quota)

	if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
		common.SysError("settle task billing error: " + settleErr.Error())
		taskErr = service.TaskErrorWrapperLocal(errors.New("failed to settle task billing"), "task_billing_settlement_failed", http.StatusInternalServerError)
		diagnostics.failed("settle", "billing_error", taskErr, true)
		return nil, taskErr
	}
	service.LogTaskConsumption(c, relayInfo, task)
	diagnostics.complete(task, result.Quota)

	return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
}

func presentTaskSubmission(c *gin.Context, outcome *taskSubmissionOutcome) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	otherRatios := outcome.RelayInfo.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	if ratiosJSON, err := common.Marshal(otherRatios); err == nil {
		c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil && pinned.Route.Render != "" {
			view, err := service.BuildTaskPluginView(outcome.Task)
			requestValue, _ := c.Get(pluginruntime.ContextKeyRouteRequest)
			requestContext, _ := requestValue.(pluginruntime.RouteRequestContext)
			if err == nil {
				viewValue, valueErr := taskPluginProtocolJSONValue(view)
				if valueErr == nil {
					if body, callErr := pinned.Plugin.Engine.CallPath(c.Request.Context(), "native", []string{pinned.Route.Render}, requestContext.JSValue(), viewValue); callErr == nil {
						diagnostics.present(outcome.Task, "native_presenter")
						c.JSON(http.StatusOK, body)
						return
					} else {
						logger.LogError(c, "task plugin native submit presenter failed: "+callErr.Error())
					}
				} else {
					logger.LogError(c, "encode task plugin native submit view failed: "+valueErr.Error())
				}
			} else {
				logger.LogError(c, "build task plugin native submit view failed: "+err.Error())
			}
		}
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint); ok && pinned.Protocol == "openai_video" && pinned.Operation.Name == "create" {
			diagnostics.present(outcome.Task, "openai_video_create")
			c.JSON(http.StatusOK, outcome.Task.ToOpenAIVideo())
			return
		}
	}
	createdAt := outcome.Task.CreatedAt
	if createdAt == 0 {
		createdAt = outcome.Task.SubmitTime
	}
	diagnostics.present(outcome.Task, "host_fallback")
	c.JSON(http.StatusOK, map[string]any{
		"id":         outcome.Task.TaskID,
		"task_id":    outcome.Task.TaskID,
		"status":     "queued",
		"model":      outcome.RelayInfo.OriginModelName,
		"created_at": createdAt,
	})
}

func respondTaskSubmissionError(c *gin.Context, taskErr *dto.TaskError) {
	newTaskPluginSubmitDiagnostics(c).presentError(taskErr)
	if middleware.RespondTaskPluginError(c, taskErr) {
		return
	}
	respondTaskError(c, taskErr)
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	taskErr.Message = service.MaskUpstreamModelInText(c, taskErr.Message)
	c.JSON(taskErr.StatusCode, taskErr)
}

// taskSubmissionAPIError adapts a task error for the shared relay error paths.
// TaskError.Error is nil for many local rejections, so fall back to the message.
func taskSubmissionAPIError(taskErr *dto.TaskError) *types.NewAPIError {
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	// 上游已接受请求（如流式提交已开始）时重试会重复生成，必须停止
	if taskErr.NoRetry {
		return false
	}
	// 插件声明的原任务渠道固定且只允许单次尝试
	if service.GetChannelConstraints(c).SuppressesRetry() {
		return false
	}
	// 客户端已断开，重试请求携带的 context 已取消，发出去也会立即失败
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
