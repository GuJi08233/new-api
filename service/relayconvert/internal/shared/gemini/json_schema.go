package gemini

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func SupportsJSONSchema(info *relaycommon.RelayInfo, model string) bool {
	if info != nil && info.ChannelMeta != nil && info.ApiType == constant.APITypeVertexAi {
		return true
	}
	return model_setting.GetGeminiVersionSetting(model) != "v1"
}

// FunctionDeclaration uses Gemini's JSON Schema field when typed parameters
// would silently lose constraints, and keeps ordinary agent tools unchanged.
func FunctionDeclaration(function dto.FunctionRequest, jsonSchema bool) dto.FunctionRequest {
	declaration := function
	declaration.ParametersJsonSchema = nil
	parameters := function.Parameters
	if schemaRequiresJSONSchema(parameters, 0) {
		if jsonSchema {
			declaration.ParametersJsonSchema = parameters
			declaration.Parameters = nil
			return declaration
		}
		common.SysLog("Gemini API v1 function parameters cannot preserve all JSON Schema constraints; using the supported subset")
	}
	if object, ok := parameters.(map[string]any); ok {
		if properties, exists := object["properties"].(map[string]any); exists && len(properties) == 0 {
			declaration.Parameters = nil
			return declaration
		}
	}
	if parameters != nil {
		declaration.Parameters = CleanFunctionParameters(parameters)
	}
	return declaration
}

func ApplyResponseSchema(config *dto.GeminiChatGenerationConfig, format json.RawMessage, jsonSchema bool) error {
	var schema dto.FormatJsonSchema
	if err := common.Unmarshal(format, &schema); err != nil {
		return err
	}
	if schemaRequiresJSONSchema(schema.Schema, 0) {
		if jsonSchema {
			var raw struct {
				Schema json.RawMessage `json:"schema"`
			}
			if err := common.Unmarshal(format, &raw); err != nil {
				return err
			}
			config.ResponseJsonSchema = raw.Schema
			config.ResponseSchema = nil
			return nil
		}
		common.SysLog("Gemini API v1 response schema cannot preserve all JSON Schema constraints; using responseSchema")
	}
	config.ResponseSchema = RemoveAdditionalProperties(schema.Schema, 0)
	config.ResponseJsonSchema = nil
	return nil
}

func schemaRequiresJSONSchema(schema any, depth int) bool {
	if depth >= geminiFunctionSchemaMaxDepth {
		return true
	}
	object, ok := schema.(map[string]any)
	if !ok {
		_, boolean := schema.(bool)
		return boolean
	}
	for key, value := range object {
		if _, supported := geminiOpenAPISchemaAllowedFields[key]; !supported {
			if key != "additionalProperties" && key != "$schema" {
				return true
			}
			continue
		}
		switch key {
		case "type":
			if _, array := value.([]any); array {
				return true
			}
		case "enum":
			values, _ := value.([]any)
			for _, item := range values {
				if _, text := item.(string); !text {
					return true
				}
			}
		case "properties":
			properties, _ := value.(map[string]any)
			for _, property := range properties {
				if schemaRequiresJSONSchema(property, depth+1) {
					return true
				}
			}
			if len(properties) == 0 {
				for field, constraint := range object {
					switch field {
					case "type", "properties", "additionalProperties", "$schema":
						continue
					case "required":
						if required, ok := constraint.([]any); ok && len(required) == 0 {
							continue
						}
					}
					return true
				}
			}
		case "items":
			if _, tuple := value.([]any); tuple || schemaRequiresJSONSchema(value, depth+1) {
				return true
			}
		case "anyOf":
			alternatives, _ := value.([]any)
			for _, alternative := range alternatives {
				if schemaRequiresJSONSchema(alternative, depth+1) {
					return true
				}
			}
		}
	}
	return false
}
