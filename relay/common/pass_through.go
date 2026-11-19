package common

import (
	"bytes"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type encodedPassThroughBody struct {
	common.ReplayableBody
	encoding string
}

func (body encodedPassThroughBody) ContentEncoding() string {
	return body.encoding
}

// GetPassThroughRequestBody 返回透传模式下发往上游的请求体。
// 当渠道开启"透传时应用模型重定向"且模型已被映射时,仅改写 body 中的
// model 字段为映射后的模型名,其余内容原样透传;否则直接透传原始 body。
func GetPassThroughRequestBody(c *gin.Context, info *RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	// 保留旧的 Decisions 别名兼容；显式渠道透传仅在开启模型重定向时改写。
	rewriteDecisionsAlias := info != nil && info.RelayFormat == types.RelayFormatOpenAIDecisions &&
		info.OriginModelName == constant.OpenAIDecisionsAlias && info.UpstreamModelName == constant.OpenAIDecisionsModel &&
		!info.ChannelSetting.PassThroughBodyEnabled
	if info == nil || info.ChannelMeta == nil ||
		(!info.ChannelSetting.PassThroughRewriteModelEnabled && !rewriteDecisionsAlias) ||
		!info.IsModelMapped || info.UpstreamModelName == "" {
		if common.DebugEnabled {
			if debugBytes, bErr := storage.Bytes(); bErr == nil {
				logger.LogDebug(c, "pass-through requestBody: %s", debugBytes)
			}
		}
		if info != nil {
			info.UpstreamRequestBodySize = storage.Size()
		}
		if original, ok := c.Get(common.KeyEncodedBodyStorage); ok {
			if encoded, ok := original.(common.BodyStorage); ok {
				if _, err := encoded.Seek(0, io.SeekStart); err != nil {
					return nil, err
				}
				if info != nil {
					info.UpstreamRequestBodySize = encoded.Size()
				}
				return encodedPassThroughBody{
					ReplayableBody: encoded,
					encoding:       c.GetString(common.KeyOriginalContentEncoding),
				}, nil
			}
		}
		return common.ReaderOnly(storage), nil
	}

	data, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(data, "model").Exists() {
		data, err = sjson.SetBytes(data, "model", info.UpstreamModelName)
		if err != nil {
			return nil, err
		}
	}
	if common.DebugEnabled {
		logger.LogDebug(c, "pass-through requestBody (model rewritten): %s", data)
	}
	info.UpstreamRequestBodySize = int64(len(data))
	return bytes.NewReader(data), nil
}
