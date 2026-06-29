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
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const sensitiveDetectionTimeout = 20 * time.Second

type sensitiveDetectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sensitiveDetectionRequest struct {
	Model          string                      `json:"model"`
	Messages       []sensitiveDetectionMessage `json:"messages"`
	Temperature    *float64                    `json:"temperature,omitempty"`
	Stream         bool                        `json:"stream"`
	ResponseFormat map[string]string           `json:"response_format,omitempty"`
}

type sensitiveDetectionChoice struct {
	Message sensitiveDetectionMessage `json:"message"`
}

type sensitiveDetectionResponse struct {
	Choices []sensitiveDetectionChoice `json:"choices"`
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

	text, ok := LatestUserPromptForSensitiveDetection(request)
	if !ok || strings.TrimSpace(text) == "" {
		setSensitiveDetectionResult(c, types.SensitiveDetectionResult{
			Status:  types.SensitiveDetectionStatusBypassed,
			Trigger: trigger,
			Reason:  "no_supported_text",
		})
		return nil
	}

	result, err := callSensitiveDetectionModel(c, text)
	result.Trigger = trigger
	result.Checked = true
	if err != nil {
		result.Status = types.SensitiveDetectionStatusErrorOpen
		result.Reason = truncateSensitiveDetectionText(err.Error(), 512)
		setSensitiveDetectionResult(c, result)
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		logger.LogWarn(c, fmt.Sprintf("sensitive detection failed open: %s", common.LocalLogPreview(err.Error())))
		return nil
	}

	if result.DetectorStatus == http.StatusOK {
		result.Status = types.SensitiveDetectionStatusAllowed
		setSensitiveDetectionResult(c, result)
		common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
		return nil
	}

	result.Status = types.SensitiveDetectionStatusBlocked
	setSensitiveDetectionResult(c, result)
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionDone, true)
	reason := result.Reason
	if reason == "" {
		reason = "prompt blocked by sensitive detection"
	}
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodePromptBlocked, http.StatusForbidden, types.ErrOptionWithSkipRetry())
}

func LatestUserPromptForSensitiveDetection(request dto.Request) (string, bool) {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return latestOpenAIUserMessage(r)
	case *dto.OpenAIResponsesRequest:
		return latestResponsesUserInput(r)
	case *dto.ClaudeRequest:
		return latestClaudeUserMessage(r)
	case *dto.GeminiChatRequest:
		return latestGeminiUserContent(r)
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

func setSensitiveDetectionResult(c *gin.Context, result types.SensitiveDetectionResult) {
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionResult, result)
}

func callSensitiveDetectionModel(c *gin.Context, text string) (types.SensitiveDetectionResult, error) {
	temperature := 0.0
	payload := sensitiveDetectionRequest{
		Model: strings.TrimSpace(setting.SensitiveDetectionModel),
		Messages: []sensitiveDetectionMessage{
			{Role: "system", Content: setting.SensitiveDetectionPrompt},
			{Role: "user", Content: text},
		},
		Temperature:    &temperature,
		Stream:         false,
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}

	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, sensitiveDetectionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sensitiveDetectionURL(setting.SensitiveDetectionBaseURL), bytes.NewReader(body))
	if err != nil {
		return types.SensitiveDetectionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(setting.SensitiveDetectionAPIKey))

	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
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

	var detectorJSON map[string]any
	if err := common.UnmarshalJsonStr(content, &detectorJSON); err != nil {
		return types.SensitiveDetectionResult{}, err
	}

	status, ok := sensitiveDetectionStatusValue(detectorJSON["status"])
	if !ok {
		return types.SensitiveDetectionResult{}, errors.New("detector JSON missing numeric status")
	}

	return types.SensitiveDetectionResult{
		DetectorStatus: status,
		Objects:        sensitiveDetectionObjects(detectorJSON),
		Reason:         sensitiveDetectionReason(detectorJSON),
	}, nil
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
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
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

func latestOpenAIUserMessage(request *dto.GeneralOpenAIRequest) (string, bool) {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		message := request.Messages[i]
		if message.Role != "user" {
			continue
		}
		text := openAIMessageText(&message)
		if strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	return "", false
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

func latestResponsesUserInput(request *dto.OpenAIResponsesRequest) (string, bool) {
	if request.Input == nil {
		return "", false
	}
	if common.GetJsonType(request.Input) == "string" {
		var text string
		if err := common.Unmarshal(request.Input, &text); err == nil && strings.TrimSpace(text) != "" {
			return text, true
		}
		return "", false
	}
	if common.GetJsonType(request.Input) != "array" {
		return "", false
	}

	var inputs []dto.Input
	if err := common.Unmarshal(request.Input, &inputs); err != nil {
		return "", false
	}
	fallback := ""
	for i := len(inputs) - 1; i >= 0; i-- {
		text := responsesInputText(inputs[i].Content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		if inputs[i].Role == "user" {
			return text, true
		}
		if fallback == "" {
			fallback = text
		}
	}
	if fallback != "" {
		return fallback, true
	}
	return "", false
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

func latestClaudeUserMessage(request *dto.ClaudeRequest) (string, bool) {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		message := request.Messages[i]
		if message.Role != "user" {
			continue
		}
		text := claudeMessageText(&message)
		if strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	return "", false
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
		if part.Type == "text" && part.GetText() != "" {
			parts = append(parts, part.GetText())
		}
	}
	return strings.Join(parts, "\n")
}

func latestGeminiUserContent(request *dto.GeminiChatRequest) (string, bool) {
	for i := len(request.Contents) - 1; i >= 0; i-- {
		content := request.Contents[i]
		if content.Role != "" && content.Role != "user" {
			continue
		}
		parts := make([]string, 0)
		for _, part := range content.Parts {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		text := strings.Join(parts, "\n")
		if strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	return "", false
}
