package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateSensitiveDetectionScopeAndSingleCall(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var payload sensitiveDetectionRequest
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		require.Equal(t, "detector-model", payload.Model)
		require.Len(t, payload.Messages, 2)
		require.Equal(t, "latest prompt", payload.Messages[1].Content)
		writeSensitiveDetectionResponse(t, w, `{"status":200}`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForTest(server.URL)
	defer restore()

	c := newSensitiveDetectionTestContext()
	err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), false, false)
	require.Nil(t, err)
	assert.Equal(t, 0, callCount)
	result, ok := common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusBypassed, result.Status)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeySensitiveDetectionDone))

	c = newSensitiveDetectionTestContext()
	err = EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, false)
	require.Nil(t, err)
	assert.Equal(t, 1, callCount)
	result, ok = common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusAllowed, result.Status)
	assert.Equal(t, "channel", result.Trigger)
	assert.True(t, result.Checked)
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeySensitiveDetectionDone))

	c = newSensitiveDetectionTestContext()
	err = EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, true)
	require.Nil(t, err)
	assert.Equal(t, 2, callCount)
	result, ok = common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusAllowed, result.Status)
	assert.Equal(t, "channel,group", result.Trigger)

	err = EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, true)
	require.Nil(t, err)
	assert.Equal(t, 2, callCount)
}

func TestEvaluateSensitiveDetectionBlocksNon200DetectorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSensitiveDetectionResponse(t, w, `{"status":451,"objects":["policy"],"reason":"blocked by policy"}`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForTest(server.URL)
	defer restore()

	c := newSensitiveDetectionTestContext()
	err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), false, true)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.StatusCode)

	result, ok := common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusBlocked, result.Status)
	assert.Equal(t, "group", result.Trigger)
	assert.Equal(t, 451, result.DetectorStatus)
	assert.Contains(t, result.Objects, "policy")
	assert.Equal(t, "blocked by policy", result.Reason)
}

func TestEvaluateSensitiveDetectionFailsOpenOnInvalidDetectorJSON(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeSensitiveDetectionResponse(t, w, `not-json`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForTest(server.URL)
	defer restore()

	c := newSensitiveDetectionTestContext()
	err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, false)
	require.Nil(t, err)
	assert.Equal(t, 1, callCount)

	result, ok := common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusErrorOpen, result.Status)
	assert.Equal(t, "channel", result.Trigger)
	assert.True(t, result.Checked)
	assert.Contains(t, result.Reason, "invalid")
}

func configureSensitiveDetectionForTest(baseURL string) func() {
	oldModel := setting.SensitiveDetectionModel
	oldBaseURL := setting.SensitiveDetectionBaseURL
	oldAPIKey := setting.SensitiveDetectionAPIKey
	oldPrompt := setting.SensitiveDetectionPrompt
	setting.SensitiveDetectionModel = "detector-model"
	setting.SensitiveDetectionBaseURL = baseURL
	setting.SensitiveDetectionAPIKey = "test-key"
	setting.SensitiveDetectionPrompt = "return json"
	return func() {
		setting.SensitiveDetectionModel = oldModel
		setting.SensitiveDetectionBaseURL = oldBaseURL
		setting.SensitiveDetectionAPIKey = oldAPIKey
		setting.SensitiveDetectionPrompt = oldPrompt
	}
}

func newSensitiveDetectionTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func newSensitiveDetectionOpenAIRequest() *dto.GeneralOpenAIRequest {
	return &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "older prompt"},
			{Role: "assistant", Content: "assistant response"},
			{Role: "user", Content: "latest prompt"},
		},
	}
}

func writeSensitiveDetectionResponse(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	response := sensitiveDetectionResponse{
		Choices: []sensitiveDetectionChoice{
			{Message: sensitiveDetectionMessage{Content: content}},
		},
	}
	data, err := common.Marshal(response)
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	require.NoError(t, err)
}
