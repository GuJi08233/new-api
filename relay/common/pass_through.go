package common

import (
	"bytes"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// GetPassThroughRequestBody 返回透传模式下发往上游的请求体。
// 当渠道开启"透传时应用模型重定向"且模型已被映射时,仅改写 body 中的
// model 字段为映射后的模型名,其余内容原样透传;否则直接透传原始 body。
func GetPassThroughRequestBody(c *gin.Context, info *RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	if info == nil || info.ChannelMeta == nil ||
		!info.ChannelSetting.PassThroughRewriteModelEnabled ||
		!info.IsModelMapped || info.UpstreamModelName == "" {
		if common.DebugEnabled {
			if debugBytes, bErr := storage.Bytes(); bErr == nil {
				logger.LogDebug(c, "pass-through requestBody: %s", debugBytes)
			}
		}
		if info != nil {
			info.UpstreamRequestBodySize = storage.Size()
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
