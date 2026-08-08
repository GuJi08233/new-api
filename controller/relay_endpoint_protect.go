package controller

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// checkModelEndpointProtection 检查模型端点保护
// 返回 nil 表示放行，返回 error 表示拦截
func checkModelEndpointProtection(c *gin.Context, modelName string, requestPath string) *types.NewAPIError {
	settings := model_setting.GetGlobalSettings()

	// 功能关闭，直接放行
	if !settings.ModelEndpointProtectEnabled {
		return nil
	}

	// 获取模型元数据
	modelMeta := getModelMeta(modelName)
	if modelMeta == nil {
		// 模型未在模型管理中配置，放行
		return nil
	}

	endpoints := strings.TrimSpace(modelMeta.Endpoints)

	// Endpoints 为空或 "*"，放行
	if endpoints == "" || endpoints == "*" || endpoints == "null" {
		return nil
	}

	// A malformed non-empty configuration must fail closed instead of silently
	// becoming an unrestricted model.
	allowedEndpoints, valid := parseEndpointsWithStatus(endpoints)
	if !valid {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Model endpoint protection: invalid endpoint config for model=%s", modelName))
		errMsg := fmt.Sprintf("模型 %s 的端点配置无效，请联系管理员", modelName)
		return types.NewError(errors.New(errMsg), types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusForbidden))
	}

	// 检查请求路径是否匹配
	if isEndpointAllowed(requestPath, allowedEndpoints) {
		return nil
	}

	// 不匹配，记录日志
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("Model endpoint protection: model=%s, path=%s, allowed=%v",
		modelName, requestPath, allowedEndpoints))

	// 返回友好错误
	errMsg := fmt.Sprintf("模型 %s 不支持当前请求路径 %s，支持的端点: %s", modelName, requestPath, strings.Join(allowedEndpoints, ", "))
	return types.NewError(errors.New(errMsg), types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusForbidden))
}

// getModelMeta 获取模型元数据（从缓存或数据库）
func getModelMeta(modelName string) *model.Model {
	if modelName == "" {
		return nil
	}

	// 先尝试精确匹配
	var modelMeta model.Model
	err := model.ReadDB().Where("model_name = ?", modelName).First(&modelMeta).Error
	if err == nil {
		return &modelMeta
	}

	// 尝试前缀匹配（支持通配符）
	var allModels []model.Model
	model.ReadDB().Find(&allModels)

	for _, m := range allModels {
		switch m.NameRule {
		case model.NameRuleExact:
			if m.ModelName == modelName {
				return &m
			}
		case model.NameRulePrefix:
			if strings.HasPrefix(modelName, m.ModelName) {
				return &m
			}
		case model.NameRuleSuffix:
			if strings.HasSuffix(modelName, m.ModelName) {
				return &m
			}
		case model.NameRuleContains:
			if strings.Contains(modelName, m.ModelName) {
				return &m
			}
		}
	}

	return nil
}

// parseEndpoints 解析 Endpoints 配置
// 支持逗号分隔格式和 JSON 格式
func parseEndpoints(endpoints string) []string {
	result, _ := parseEndpointsWithStatus(endpoints)
	return result
}

// parseEndpointsWithStatus accepts the model editor's object format, legacy
// arrays/strings, and comma-separated endpoint names. The boolean reports
// whether a non-empty configuration was syntactically and structurally valid.
func parseEndpointsWithStatus(endpoints string) ([]string, bool) {
	endpoints = strings.TrimSpace(endpoints)
	if endpoints == "" || endpoints == "*" || endpoints == "null" {
		return nil, true
	}
	if strings.HasPrefix(endpoints, "{") || strings.HasPrefix(endpoints, "[") || strings.HasPrefix(endpoints, "\"") {
		return parseJSONEndpointsWithStatus(endpoints)
	}

	parts := strings.Split(endpoints, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if endpoint := normalizeEndpointToken(part); endpoint != "" {
			result = append(result, endpoint)
		}
	}
	return deduplicateEndpoints(result), true
}

func parseJSONEndpointsWithStatus(endpoints string) ([]string, bool) {
	var raw any
	if err := common.Unmarshal([]byte(endpoints), &raw); err != nil {
		return nil, false
	}

	result := make([]string, 0)
	switch value := raw.(type) {
	case map[string]any:
		if _, hasPath := value["path"]; hasPath {
			path, ok := endpointPathFromEntry("", value)
			if !ok {
				return nil, false
			}
			if path != "" {
				result = append(result, path)
			}
			break
		}
		result = make([]string, 0, len(value))
		for key, entry := range value {
			path, ok := endpointPathFromEntry(key, entry)
			if !ok {
				return nil, false
			}
			if path != "" {
				result = append(result, path)
			}
		}
	case []any:
		result = make([]string, 0, len(value))
		for _, entry := range value {
			switch item := entry.(type) {
			case string:
				if path := normalizeEndpointToken(item); path != "" {
					result = append(result, path)
				}
			case map[string]any:
				path, ok := endpointPathFromEntry("", item)
				if !ok {
					return nil, false
				}
				if path != "" {
					result = append(result, path)
				}
			default:
				return nil, false
			}
		}
	case string:
		if path := normalizeEndpointToken(value); path != "" {
			result = append(result, path)
		}
	default:
		return nil, false
	}
	return deduplicateEndpoints(result), true
}

func endpointPathFromEntry(key string, entry any) (string, bool) {
	key = strings.TrimSpace(key)
	if object, ok := entry.(map[string]any); ok {
		if rawPath, exists := object["path"]; exists {
			path, ok := rawPath.(string)
			if !ok || strings.TrimSpace(path) == "" {
				return "", false
			}
			return normalizeEndpointToken(path), true
		}
	}
	if path, ok := entry.(string); ok {
		path = normalizeEndpointToken(path)
		if path != "" {
			return path, true
		}
	}
	if key == "" {
		return "", false
	}
	return normalizeEndpointToken(key), true
}

func normalizeEndpointToken(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || endpoint == "*" {
		return ""
	}
	switch endpoint {
	case "embedding":
		return "/v1/embeddings"
	case "rerank":
		return "/v1/rerank"
	case "gemini-embed", "gemini_embed":
		return "/v1beta/models/{model}:embedContent"
	case "gemini-batch-embed", "gemini_batch_embed":
		return "/v1beta/models/{model}:batchEmbedContents"
	case "audio-transcriptions":
		return "/v1/audio/transcriptions"
	case "audio-speech":
		return "/v1/audio/speech"
	case "moderations":
		return "/v1/moderations"
	case "files":
		return "/v1/files"
	case "openai-video":
		return "/v1/videos"
	}
	if info, ok := common.GetDefaultEndpointInfo(constant.EndpointType(endpoint)); ok {
		return info.Path
	}
	return endpoint
}

func deduplicateEndpoints(endpoints []string) []string {
	seen := make(map[string]struct{}, len(endpoints))
	result := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || endpoint == "*" {
			continue
		}
		if _, exists := seen[endpoint]; exists {
			continue
		}
		seen[endpoint] = struct{}{}
		result = append(result, endpoint)
	}
	sort.Strings(result)
	return result
}

// isEndpointAllowed 检查请求路径是否在允许的端点列表中
func isEndpointAllowed(requestPath string, allowedEndpoints []string) bool {
	if len(allowedEndpoints) == 0 {
		return true
	}

	// 标准化路径
	requestPath = strings.TrimSuffix(requestPath, "/")

	for _, endpoint := range allowedEndpoints {
		endpoint = strings.TrimSuffix(endpoint, "/")

		// 精确匹配
		if requestPath == endpoint {
			return true
		}

		// 前缀匹配（endpoint 以 / 结尾或请求路径以 endpoint 开头）
		if strings.HasSuffix(endpoint, "/") && strings.HasPrefix(requestPath, endpoint) {
			return true
		}
		if strings.HasPrefix(requestPath, endpoint+"/") {
			return true
		}

		// Gemini 路径归一化匹配：
		// /v1/models/{model}:streamGenerateContent ↔ /v1beta/models/{model}:generateContent
		if geminiPathMatch(requestPath, endpoint) {
			return true
		}
	}

	return false
}

// geminiPathMatch 对 Gemini 风格路径做归一化比较
// 处理 /v1/ vs /v1beta/ 版本差异和 streamGenerateContent vs generateContent 流式差异
func geminiPathMatch(requestPath, endpoint string) bool {
	// 两个路径都必须包含 :generateContent（或 :streamGenerateContent）
	reqAction := extractGeminiAction(requestPath)
	epAction := extractGeminiAction(endpoint)
	if reqAction == "" || epAction == "" {
		return false
	}

	// 归一化 action：streamGenerateContent → generateContent
	reqAction = normalizeGeminiAction(reqAction)
	epAction = normalizeGeminiAction(epAction)

	// action 不匹配
	if reqAction != epAction {
		return false
	}

	// 提取版本之后的路径部分（models/xxx:action）
	reqBase := stripVersionPrefix(requestPath)
	epBase := stripVersionPrefix(endpoint)

	// 归一化模型名：将实际模型名替换为 {model} 占位符后再比较
	reqBase = normalizeGeminiModelPath(reqBase)
	epBase = normalizeGeminiModelPath(epBase)

	return reqBase == epBase
}

// extractGeminiAction 提取 Gemini 路径中 : 后面的 action（不含查询参数）
func extractGeminiAction(path string) string {
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		return ""
	}
	action := path[idx+1:]
	// 去掉查询参数
	if qi := strings.Index(action, "?"); qi != -1 {
		action = action[:qi]
	}
	// 必须是合法的 Gemini action
	if action == "generateContent" || action == "streamGenerateContent" ||
		action == "countTokens" || action == "embedContent" || action == "batchEmbedContents" {
		return action
	}
	return ""
}

// normalizeGeminiAction 将 streamGenerateContent 归一化为 generateContent
func normalizeGeminiAction(action string) string {
	if action == "streamGenerateContent" {
		return "generateContent"
	}
	return action
}

// stripVersionPrefix 去掉 /v1/、/v1beta/、/v1alpha/ 版本前缀
func stripVersionPrefix(path string) string {
	for _, prefix := range []string{"/v1beta/", "/v1alpha/", "/v1/"} {
		if strings.HasPrefix(path, prefix) {
			return path[len(prefix):]
		}
	}
	return path
}

// normalizeGeminiModelPath 将 models/xxx:action 归一化为 models/{model}:action
func normalizeGeminiModelPath(path string) string {
	const modelsPrefix = "models/"
	idx := strings.Index(path, modelsPrefix)
	if idx == -1 {
		return path
	}
	afterModels := path[idx+len(modelsPrefix):]
	colonIdx := strings.Index(afterModels, ":")
	if colonIdx == -1 {
		return path
	}
	action := afterModels[colonIdx+1:]
	if queryIdx := strings.Index(action, "?"); queryIdx != -1 {
		action = action[:queryIdx]
	}
	return path[:idx] + modelsPrefix + "{model}:" + normalizeGeminiAction(action)
}
