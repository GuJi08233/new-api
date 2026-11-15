package dto

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// DecisionsRequest 保留各提供商的原生问题结构。
type DecisionsRequest interface {
	Request
	Validate() error
}

// ParseDecisionsRequest 同时用于客户端校验和参数覆盖后的上游请求校验。
func ParseDecisionsRequest(data []byte, format types.RelayFormat) (DecisionsRequest, error) {
	var request DecisionsRequest
	switch format {
	case types.RelayFormatGeminiDecisions:
		request = &GeminiDecisionsRequest{}
	case types.RelayFormatOpenAIDecisions:
		request = &OpenAIDecisionsRequest{}
	default:
		return nil, fmt.Errorf("unsupported decisions format: %s", format)
	}
	if err := common.Unmarshal(data, request); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return request, nil
}
