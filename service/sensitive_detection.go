package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type sensitiveDetectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sensitiveDetectionRequest struct {
	Model          string                      `json:"model"`
	Messages       []sensitiveDetectionMessage `json:"messages"`
	Temperature    *float64                    `json:"temperature,omitempty"`
	MaxTokens      *int                        `json:"max_tokens,omitempty"`
	Stream         bool                        `json:"stream"`
	Thinking       map[string]string           `json:"thinking,omitempty"`
	DoSample       *bool                       `json:"do_sample,omitempty"`
	ResponseFormat map[string]string           `json:"response_format,omitempty"`
}

type sensitiveDetectionChoice struct {
	Message sensitiveDetectionMessage `json:"message"`
}

type sensitiveDetectionResponse struct {
	Choices []sensitiveDetectionChoice `json:"choices"`
}

type SensitiveDetectionConnectionTestConfig struct {
	Model          string
	BaseURL        string
	APIKey         string
	Prompt         string
	TimeoutSeconds int
}

func EvaluateSensitiveDetection(c *gin.Context, request dto.Request, channelEnabled bool, groupEnabled bool) *types.NewAPIError {
	if common.GetContextKeyBool(c, constant.ContextKeySensitiveDetectionDone) {
		return nil
	}

	trigger := sensitiveDetectionTrigger(channelEnabled, groupEnabled)
	if trigger == "" {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status: types.SensitiveDetectionStatusBypassed,
		})
		return nil
	}

	if !setting.SensitiveDetectionConfigured() {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status:  types.SensitiveDetectionStatusBypassed,
			Trigger: trigger,
			Reason:  "detector_not_configured",
		})
		return nil
	}

	text, ok := SensitiveDetectionRequestText(request)
	text = strings.TrimSpace(text)
	if !ok || text == "" {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status:  types.SensitiveDetectionStatusBypassed,
			Trigger: trigger,
			Reason:  "no_supported_text",
		})
		return nil
	}

	if apiErr := rejectOversizedSensitiveDetectionText(c, trigger, text); apiErr != nil {
		return apiErr
	}

	// 缓存命中（allowed/blocked）直接复用，不再调用检测模型、也不消耗熔断/限流配额。
	if cached, found := loadCachedSensitiveDetectionResult(trigger, text); found {
		cached.Trigger = trigger
		cached.Checked = true
		setSensitiveDetectionResult(c, cached)
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		if cached.Status == types.SensitiveDetectionStatusBlocked {
			reason := cached.Reason
			if reason == "" {
				reason = "prompt blocked by sensitive detection"
			}
			return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodePromptBlocked, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		return nil
	}

	// 命中熔断时直接放行：检测模型故障不应把网关拖死。
	if !sensitiveDetectionBreakerAllows() {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status:  types.SensitiveDetectionStatusErrorOpen,
			Trigger: trigger,
			Checked: true,
			Reason:  "breaker_open",
		})
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		return nil
	}

	if apiErr := enforceSensitiveDetectionSubjectRateLimit(c, trigger); apiErr != nil {
		return apiErr
	}

	// 限流：超过配置的 RPM 上限时放行（不检测），避免把检测模型打爆。
	if !allowSensitiveDetectionCall(c.Request.Context()) {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status:  types.SensitiveDetectionStatusErrorOpen,
			Trigger: trigger,
			Checked: true,
			Reason:  "rate_limited",
		})
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		return nil
	}

	result, err := callSensitiveDetectionModel(c, text)
	result.Trigger = trigger
	result.Checked = true
	if err != nil {
		// 仅调用失败（网络/超时/非 2xx）计入熔断；业务拦截(status!=200)不算失败。
		recordSensitiveDetectionCallOutcome(false)
		result.Status = types.SensitiveDetectionStatusErrorOpen
		result.Reason = truncateSensitiveDetectionText(err.Error(), 512)
		setSensitiveDetectionResult(c, result)
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		logger.LogWarn(c, fmt.Sprintf("sensitive detection failed open: %s", common.LocalLogPreview(err.Error())))
		return nil
	}

	// 正常拿到 status JSON 即视为调用成功，重置熔断失败计数。
	recordSensitiveDetectionCallOutcome(true)

	if result.DetectorStatus == http.StatusOK {
		result.Status = types.SensitiveDetectionStatusAllowed
		storeCachedSensitiveDetectionResult(trigger, text, result)
		setSensitiveDetectionResult(c, result)
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		return nil
	}

	result.Status = types.SensitiveDetectionStatusBlocked
	storeCachedSensitiveDetectionResult(trigger, text, result)
	setSensitiveDetectionResult(c, result)
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
	reason := result.Reason
	if reason == "" {
		reason = "prompt blocked by sensitive detection"
	}
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodePromptBlocked, http.StatusForbidden, types.ErrOptionWithSkipRetry())
}

func TestSensitiveDetectionConnection(ctx context.Context, config SensitiveDetectionConnectionTestConfig) (types.SensitiveDetectionResult, error) {
	if strings.TrimSpace(config.Model) == "" {
		return types.SensitiveDetectionResult{}, errors.New("detector model is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return types.SensitiveDetectionResult{}, errors.New("detector base url is required")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return types.SensitiveDetectionResult{}, errors.New("detector api key is required")
	}
	if strings.TrimSpace(config.Prompt) == "" {
		return types.SensitiveDetectionResult{}, errors.New("detector prompt is required")
	}
	return callSensitiveDetectionModelWithConfig(ctx, config, "This is a connectivity test request.")
}

func LatestUserPromptForSensitiveDetection(request dto.Request) (string, bool) {
	return SensitiveDetectionRequestText(request)
}

func SensitiveDetectionRequestText(request dto.Request) (string, bool) {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return openAIRequestText(r)
	case *dto.OpenAIResponsesRequest:
		return responsesRequestText(r)
	case *dto.ClaudeRequest:
		return claudeRequestText(r)
	case *dto.GeminiChatRequest:
		return geminiRequestText(r)
	default:
		return "", false
	}
}

func sensitiveDetectionTrigger(channelEnabled bool, groupEnabled bool) string {
	if channelEnabled && groupEnabled {
		return "channel,group"
	}
	if channelEnabled {
		return "channel"
	}
	if groupEnabled {
		return "group"
	}
	return ""
}

func rejectOversizedSensitiveDetectionText(c *gin.Context, trigger, text string) *types.NewAPIError {
	maxRunes := setting.SensitiveDetectionMaxRequestRunes
	if maxRunes <= 0 {
		return nil
	}
	runeCount := utf8.RuneCountInString(text)
	if runeCount <= maxRunes {
		return nil
	}
	reason := fmt.Sprintf("sensitive_detection_request_too_large: %d>%d runes", runeCount, maxRunes)
	setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
		Status:         types.SensitiveDetectionStatusBlocked,
		Trigger:        trigger,
		Checked:        true,
		Reason:         reason,
		DetectorStatus: http.StatusRequestEntityTooLarge,
	})
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
	return types.NewErrorWithStatusCode(errors.New("sensitive detection request too large"), types.ErrorCodeInvalidRequest, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
}

func enforceSensitiveDetectionSubjectRateLimit(c *gin.Context, trigger string) *types.NewAPIError {
	if c == nil {
		return nil
	}
	ctx := context.Background()
	if c.Request != nil && c.Request.Context() != nil {
		ctx = c.Request.Context()
	}
	if !allowSensitiveDetectionScopedCall(ctx, "token", c.GetInt("token_id"), setting.SensitiveDetectionTokenRPM) {
		return sensitiveDetectionSubjectRateLimitError(c, trigger, "token")
	}
	if !allowSensitiveDetectionScopedCall(ctx, "user", c.GetInt("id"), setting.SensitiveDetectionUserRPM) {
		return sensitiveDetectionSubjectRateLimitError(c, trigger, "user")
	}
	return nil
}

func allowSensitiveDetectionScopedCall(ctx context.Context, scope string, id int, rpm int) bool {
	if rpm <= 0 || id <= 0 {
		return true
	}
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limiterInstance := limiter.New(ctx, common.RDB)
	allowed, err := limiterInstance.Allow(ctx, fmt.Sprintf("sensitive_detection:%s_rpm:%d", scope, id),
		limiter.WithCapacity(int64(rpm)),
		limiter.WithRate(sensitiveDetectionRateLimitRefill(rpm)),
		limiter.WithRequested(1),
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("sensitive detection %s rate limiter error (fail-open): %s", scope, err.Error()))
		return true
	}
	return allowed
}

func sensitiveDetectionRateLimitRefill(rpm int) int64 {
	refill := rpm / 60
	if refill < 1 {
		refill = 1
	}
	return int64(refill)
}

func sensitiveDetectionSubjectRateLimitError(c *gin.Context, trigger, scope string) *types.NewAPIError {
	reason := "sensitive_detection_" + scope + "_rate_limited"
	setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
		Status:         types.SensitiveDetectionStatusBlocked,
		Trigger:        trigger,
		Checked:        true,
		Reason:         reason,
		DetectorStatus: http.StatusTooManyRequests,
	})
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
	return types.NewErrorWithStatusCode(errors.New("sensitive detection rate limit exceeded"), types.ErrorCodeRateLimitExceeded, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
}

// allowSensitiveDetectionCall 基于 Redis 令牌桶对发往检测模型的调用做 RPM 限流。
// 返回 false 表示当前已超限，调用方应 fail-open 放行（不检测、不拦截）。
// 限流策略为 fail-open：配置为 0（无限）、Redis 未启用、或限流器自身出错时一律放行。
// 多实例共享同一个 Redis 桶（key 固定），因此全站 RPM 上限准确。
func allowSensitiveDetectionCall(ctx context.Context) bool {
	rpm := setting.SensitiveDetectionRPM
	if rpm <= 0 {
		return true
	}
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limiterInstance := limiter.New(ctx, common.RDB)
	allowed, err := limiterInstance.Allow(ctx, "sensitive_detection:rpm",
		limiter.WithCapacity(int64(rpm)),
		limiter.WithRate(sensitiveDetectionRateLimitRefill(rpm)),
		limiter.WithRequested(1),
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("sensitive detection rate limiter error (fail-open): %s", err.Error()))
		return true
	}
	return allowed
}

func setSensitiveDetectionResult(c *gin.Context, result types.SensitiveDetectionResult) {
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionResult, result)
}

func callSensitiveDetectionModel(c *gin.Context, text string) (types.SensitiveDetectionResult, error) {
	config := SensitiveDetectionConnectionTestConfig{
		Model:          setting.SensitiveDetectionModel,
		BaseURL:        setting.SensitiveDetectionBaseURL,
		APIKey:         setting.SensitiveDetectionAPIKey,
		Prompt:         setting.SensitiveDetectionPrompt,
		TimeoutSeconds: setting.SensitiveDetectionTimeoutSeconds,
	}
	ctx := context.Background()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		ctx = c.Request.Context()
	}
	return callSensitiveDetectionModelWithConfig(ctx, config, text)
}

func callSensitiveDetectionModelWithConfig(ctx context.Context, config SensitiveDetectionConnectionTestConfig, text string) (types.SensitiveDetectionResult, error) {
	temperature := 0.0
	maxTokens := 8
	payload := sensitiveDetectionRequest{
		Model: strings.TrimSpace(config.Model),
		Messages: []sensitiveDetectionMessage{
			{Role: "system", Content: config.Prompt},
			{Role: "user", Content: text},
		},
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Stream:      false,
		// 不强制 response_format=json_object：提示词可能要求模型返回裸状态码（如 "200"），
		// 由提示词自行控制返回格式；解析层同时兼容 JSON 与裸整数两种返回。
	}
	if sensitiveDetectionUsesBigModelHints(config.BaseURL, config.Model) {
		doSample := false
		payload.Thinking = map[string]string{"type": "disabled"}
		payload.DoSample = &doSample
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := sensitiveDetectionTimeoutDurationForSeconds(config.TimeoutSeconds)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sensitiveDetectionURL(config.BaseURL), bytes.NewReader(body))
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(config.APIKey))

	client := getSensitiveDetectionClient()
	resp, err := client.Do(req)
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return types.SensitiveDetectionResult{}, fmt.Errorf("detector returned status %d", resp.StatusCode)
	}

	var detectorResp sensitiveDetectionResponse
	if err := common.Unmarshal(respBody, &detectorResp); err != nil {
		return types.SensitiveDetectionResult{}, err
	}
	if len(detectorResp.Choices) == 0 {
		return types.SensitiveDetectionResult{}, errors.New("detector returned no choices")
	}
	content := strings.TrimSpace(detectorResp.Choices[0].Message.Content)
	if content == "" {
		return types.SensitiveDetectionResult{}, errors.New("detector returned empty content")
	}

	// 优先按 JSON 对象解析（含 status 字段，可携带 reason/objects 等详情）。
	// 失败时回退到裸整数解析：模型可直接返回 "200" / "499" 这样的纯状态码，
	// 此时没有 reason/objects，仅依据数字判定放行或拦截。
	if status, objects, reason, ok := parseSensitiveDetectionJSON(content); ok {
		return types.SensitiveDetectionResult{
			DetectorStatus: status,
			Objects:        objects,
			Reason:         reason,
		}, nil
	}
	if status, ok := parseSensitiveDetectionRawInt(content); ok {
		return types.SensitiveDetectionResult{
			DetectorStatus: status,
		}, nil
	}
	return types.SensitiveDetectionResult{}, errors.New("detector content is neither JSON with status nor a bare status integer")
}

func sensitiveDetectionUsesBigModelHints(baseURL, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(model, "glm-") {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Hostname()), "bigmodel.cn")
}

// parseSensitiveDetectionJSON 尝试把 content 当 JSON 对象解析并读取 status 字段。
// 成功返回 (status, objects, reason, true)；不是合法 JSON 或缺少 status 返回 (_, "", "", false)。
func parseSensitiveDetectionJSON(content string) (int, string, string, bool) {
	var detectorJSON map[string]any
	if err := common.UnmarshalJsonStr(content, &detectorJSON); err != nil {
		return 0, "", "", false
	}
	status, ok := sensitiveDetectionStatusValue(detectorJSON["status"])
	if !ok {
		return 0, "", "", false
	}
	return status, sensitiveDetectionObjects(detectorJSON), sensitiveDetectionReason(detectorJSON), true
}

// parseSensitiveDetectionRawInt 尝试把 content 当裸状态码整数解析。
// 兼容 "200"、"499"、以及前后偶发的空白/标点（如 "200." 或 "Status: 200"）。
func parseSensitiveDetectionRawInt(content string) (int, bool) {
	// 先去掉常见的前缀文字与首尾标点，提取末尾的数字串。
	trimmed := strings.Trim(strings.TrimSpace(content), ".;,:。；，： \n\r\t")
	if trimmed == "" {
		return 0, false
	}
	// 取最后一段连续数字（处理 "Status: 200" 这类），但纯数字直接解析。
	fields := strings.Fields(trimmed)
	candidate := trimmed
	if len(fields) > 0 {
		candidate = fields[len(fields)-1]
	}
	candidate = strings.Trim(candidate, ".;,:。；，：")
	status, err := strconv.Atoi(candidate)
	if err != nil {
		return 0, false
	}
	return status, true
}

func sensitiveDetectionURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "/v1/chat/completions"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + "/v1/chat/completions"
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(path, "/v1") || strings.HasSuffix(path, "/v4") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

func sensitiveDetectionStatusValue(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case string:
		status, err := strconv.Atoi(strings.TrimSpace(typed))
		return status, err == nil
	default:
		return 0, false
	}
}

func sensitiveDetectionObjects(payload map[string]any) string {
	for _, key := range []string{"objects", "object", "labels", "categories", "violations", "flagged_objects"} {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		data, err := common.Marshal(value)
		if err != nil {
			continue
		}
		return truncateSensitiveDetectionText(string(data), 1024)
	}
	return ""
}

func sensitiveDetectionReason(payload map[string]any) string {
	for _, key := range []string{"reason", "message", "detail"} {
		if value, ok := payload[key].(string); ok {
			return truncateSensitiveDetectionText(value, 512)
		}
	}
	return ""
}

func truncateSensitiveDetectionText(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes])
}

func openAIRequestText(request *dto.GeneralOpenAIRequest) (string, bool) {
	parts := make([]string, 0, len(request.Messages)+4)
	appendSensitiveDetectionAnyText(&parts, "prompt", request.Prompt)
	appendSensitiveDetectionAnyText(&parts, "input", request.Input)
	appendSensitiveDetectionText(&parts, "instruction", request.Instruction)
	appendSensitiveDetectionAnyText(&parts, "prefix", request.Prefix)
	appendSensitiveDetectionAnyText(&parts, "suffix", request.Suffix)
	for _, message := range request.Messages {
		text := openAIMessageText(&message)
		appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(message.Role), text)
		if message.ReasoningContent != nil {
			appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(message.Role)+".reasoning_content", *message.ReasoningContent)
		}
		if message.Reasoning != nil {
			appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(message.Role)+".reasoning", *message.Reasoning)
		}
		appendSensitiveDetectionRawJSONText(&parts, sensitiveDetectionRoleLabel(message.Role)+".tool_calls", message.ToolCalls)
	}
	if len(request.Functions) > 0 {
		appendSensitiveDetectionRawJSONText(&parts, "functions", request.Functions)
	}
	if len(request.FunctionCall) > 0 {
		appendSensitiveDetectionRawJSONText(&parts, "function_call", request.FunctionCall)
	}
	if len(request.Tools) > 0 {
		if data, err := common.Marshal(request.Tools); err == nil {
			appendSensitiveDetectionText(&parts, "tools", string(data))
		}
	}
	return sensitiveDetectionJoinedText(parts)
}

func openAIMessageText(message *dto.Message) string {
	if message == nil {
		return ""
	}
	parts := make([]string, 0)
	for _, part := range message.ParseContent() {
		if part.Type == dto.ContentTypeText && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func responsesRequestText(request *dto.OpenAIResponsesRequest) (string, bool) {
	parts := make([]string, 0)
	appendSensitiveDetectionRawJSONText(&parts, "instructions", request.Instructions)
	appendSensitiveDetectionRawJSONText(&parts, "prompt", request.Prompt)
	appendSensitiveDetectionRawJSONText(&parts, "tools", request.Tools)
	appendSensitiveDetectionRawJSONText(&parts, "tool_choice", request.ToolChoice)
	if request.Input == nil {
		return sensitiveDetectionJoinedText(parts)
	}
	if common.GetJsonType(request.Input) == "string" {
		var text string
		if err := common.Unmarshal(request.Input, &text); err == nil {
			appendSensitiveDetectionText(&parts, "input", text)
		}
		return sensitiveDetectionJoinedText(parts)
	}
	if common.GetJsonType(request.Input) != "array" {
		appendSensitiveDetectionRawJSONText(&parts, "input", request.Input)
		return sensitiveDetectionJoinedText(parts)
	}

	var inputs []dto.Input
	if err := common.Unmarshal(request.Input, &inputs); err != nil {
		appendSensitiveDetectionRawJSONText(&parts, "input", request.Input)
		return sensitiveDetectionJoinedText(parts)
	}
	for _, input := range inputs {
		label := input.Role
		if label == "" {
			label = input.Type
		}
		appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(label), responsesInputText(input.Content))
	}
	return sensitiveDetectionJoinedText(parts)
}

func responsesInputText(content json.RawMessage) string {
	switch common.GetJsonType(content) {
	case "string":
		var text string
		if err := common.Unmarshal(content, &text); err == nil {
			return text
		}
	case "array":
		var items []map[string]any
		if err := common.Unmarshal(content, &items); err != nil {
			return ""
		}
		parts := make([]string, 0)
		for _, item := range items {
			if item["type"] != "input_text" && item["type"] != "text" {
				continue
			}
			if text, ok := item["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func claudeRequestText(request *dto.ClaudeRequest) (string, bool) {
	parts := make([]string, 0, len(request.Messages)+3)
	appendSensitiveDetectionText(&parts, "prompt", request.Prompt)
	appendSensitiveDetectionText(&parts, "system", claudeSystemText(request))
	appendSensitiveDetectionAnyText(&parts, "tools", request.Tools)
	appendSensitiveDetectionAnyText(&parts, "tool_choice", request.ToolChoice)
	for _, message := range request.Messages {
		text := claudeMessageText(&message)
		appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(message.Role), text)
	}
	return sensitiveDetectionJoinedText(parts)
}

func claudeSystemText(request *dto.ClaudeRequest) string {
	if request == nil || request.System == nil {
		return ""
	}
	if request.IsStringSystem() {
		return request.GetStringSystem()
	}
	parts := make([]string, 0)
	for _, part := range request.ParseSystem() {
		if part.Type == "text" {
			appendSensitiveDetectionText(&parts, "", part.GetText())
		}
	}
	return strings.Join(parts, "\n")
}

func claudeMessageText(message *dto.ClaudeMessage) string {
	if message == nil {
		return ""
	}
	if message.IsStringContent() {
		return message.GetStringContent()
	}
	content, _ := message.ParseContent()
	parts := make([]string, 0)
	for _, part := range content {
		switch part.Type {
		case "text":
			parts = append(parts, part.GetText())
		case "tool_use":
			appendSensitiveDetectionText(&parts, "tool_use.name", part.Name)
			appendSensitiveDetectionAnyText(&parts, "tool_use.input", part.Input)
		case "tool_result":
			appendSensitiveDetectionAnyText(&parts, "tool_result.content", part.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func geminiRequestText(request *dto.GeminiChatRequest) (string, bool) {
	parts := make([]string, 0, len(request.Contents)+1)
	if request.SystemInstructions != nil {
		appendSensitiveDetectionText(&parts, "system", geminiContentText(request.SystemInstructions))
	}
	for _, content := range request.Contents {
		appendSensitiveDetectionText(&parts, sensitiveDetectionRoleLabel(content.Role), geminiContentText(&content))
	}
	appendSensitiveDetectionRawJSONText(&parts, "tools", request.Tools)
	return sensitiveDetectionJoinedText(parts)
}

func geminiContentText(content *dto.GeminiChatContent) string {
	if content == nil {
		return ""
	}
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		appendSensitiveDetectionText(&parts, "", part.Text)
		if part.FunctionCall != nil {
			appendSensitiveDetectionAnyText(&parts, "function_call", part.FunctionCall)
		}
		if part.FunctionResponse != nil {
			appendSensitiveDetectionAnyText(&parts, "function_response", part.FunctionResponse)
		}
		if part.ExecutableCode != nil {
			appendSensitiveDetectionAnyText(&parts, "executable_code", part.ExecutableCode)
		}
		if part.CodeExecutionResult != nil {
			appendSensitiveDetectionAnyText(&parts, "code_execution_result", part.CodeExecutionResult)
		}
	}
	return strings.Join(parts, "\n")
}

func appendSensitiveDetectionText(parts *[]string, label string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	label = strings.TrimSpace(label)
	if label == "" {
		*parts = append(*parts, text)
		return
	}
	*parts = append(*parts, label+":\n"+text)
}

func appendSensitiveDetectionAnyText(parts *[]string, label string, value any) {
	appendSensitiveDetectionText(parts, label, sensitiveDetectionAnyText(value))
}

func appendSensitiveDetectionRawJSONText(parts *[]string, label string, raw json.RawMessage) {
	appendSensitiveDetectionText(parts, label, sensitiveDetectionRawJSONText(raw))
}

func sensitiveDetectionAnyText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.RawMessage:
		return sensitiveDetectionRawJSONText(v)
	case []string:
		return strings.Join(v, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			appendSensitiveDetectionText(&parts, "", sensitiveDetectionAnyText(item))
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			appendSensitiveDetectionText(&parts, key, sensitiveDetectionAnyText(v[key]))
		}
		return strings.Join(parts, "\n")
	default:
		if data, err := common.Marshal(value); err == nil {
			return sensitiveDetectionRawJSONText(json.RawMessage(data))
		}
		return fmt.Sprintf("%v", value)
	}
}

func sensitiveDetectionRawJSONText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if common.GetJsonType(raw) == "string" {
		var text string
		if err := common.Unmarshal(raw, &text); err == nil {
			return text
		}
	}
	return string(raw)
}

func sensitiveDetectionRoleLabel(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "message"
	}
	return role
}

func sensitiveDetectionJoinedText(parts []string) (string, bool) {
	text := strings.TrimSpace(strings.Join(parts, "\n\n"))
	return text, text != ""
}
