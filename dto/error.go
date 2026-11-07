package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

//type OpenAIError struct {
//	Message string `json:"message"`
//	Type    string `json:"type"`
//	Param   string `json:"param"`
//	Code    any    `json:"code"`
//}

type OpenAIErrorWithStatusCode struct {
	Error      types.OpenAIError `json:"error"`
	StatusCode int               `json:"status_code"`
	LocalError bool
}

type GeneralErrorResponse struct {
	Error    json.RawMessage `json:"error"`
	Message  string          `json:"message"`
	Msg      string          `json:"msg"`
	Err      string          `json:"err"`
	ErrorMsg string          `json:"error_msg"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	// Detail is either a plain string or, for providers such as TypeSafe's
	// System One, an object carrying error_type and message. Keeping it raw lets
	// both shapes unmarshal instead of failing the whole error body on the
	// object form.
	Detail json.RawMessage `json:"detail,omitempty"`
	Header struct {
		Message string `json:"message"`
	} `json:"header"`
	Response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

func (e GeneralErrorResponse) TryToOpenAIError() *types.OpenAIError {
	var openAIError types.OpenAIError
	if len(e.Error) > 0 {
		err := common.Unmarshal(e.Error, &openAIError)
		if err == nil && openAIError.Message != "" {
			return &openAIError
		}
	}
	return nil
}

func (e GeneralErrorResponse) ToMessage() string {
	if len(e.Error) > 0 {
		switch common.GetJsonType(e.Error) {
		case "object":
			var openAIError types.OpenAIError
			err := common.Unmarshal(e.Error, &openAIError)
			if err == nil && openAIError.Message != "" {
				return openAIError.Message
			}
		case "string":
			var msg string
			err := common.Unmarshal(e.Error, &msg)
			if err == nil && msg != "" {
				return msg
			}
		default:
			return string(e.Error)
		}
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != "" {
		return e.Err
	}
	if e.ErrorMsg != "" {
		return e.ErrorMsg
	}
	// detail is either a plain string or, for providers such as TypeSafe's System
	// One, an object of the form {"error_type": "...", "message": "..."} whose
	// message is the only place the reason for a 400/422 is written down.
	switch common.GetJsonType(e.Detail) {
	case "string":
		var message string
		if err := common.Unmarshal(e.Detail, &message); err == nil && message != "" {
			return message
		}
	case "object":
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(e.Detail, &nested); err == nil && nested.Message != "" {
			return nested.Message
		}
	}
	if e.Header.Message != "" {
		return e.Header.Message
	}
	if e.Response.Error.Message != "" {
		return e.Response.Error.Message
	}
	return ""
}

// DetailErrorType returns the error_type carried by an object-shaped detail.
// Providers such as TypeSafe classify failures there (validation_error,
// insufficient_credits, ...) and that classification is more useful to clients
// and error logs than a generic status-code code.
func (e GeneralErrorResponse) DetailErrorType() string {
	if common.GetJsonType(e.Detail) != "object" {
		return ""
	}
	var nested struct {
		ErrorType string `json:"error_type"`
	}
	if err := common.Unmarshal(e.Detail, &nested); err != nil {
		return ""
	}
	return nested.ErrorType
}
