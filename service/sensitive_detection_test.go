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
	oldCacheEnabled := setting.SensitiveDetectionCacheEnabled
	oldBreakerThreshold := setting.SensitiveDetectionBreakerThreshold
	setting.SensitiveDetectionModel = "detector-model"
	setting.SensitiveDetectionBaseURL = baseURL
	setting.SensitiveDetectionAPIKey = "test-key"
	setting.SensitiveDetectionPrompt = "return json"
	// 这些用例直接验证检测模型调用契约，关闭缓存与熔断以保证每个用例独立、可复现。
	setting.SensitiveDetectionCacheEnabled = false
	setting.SensitiveDetectionBreakerThreshold = 0
	resetSensitiveDetectionBreakerForTest()
	return func() {
		setting.SensitiveDetectionModel = oldModel
		setting.SensitiveDetectionBaseURL = oldBaseURL
		setting.SensitiveDetectionAPIKey = oldAPIKey
		setting.SensitiveDetectionPrompt = oldPrompt
		setting.SensitiveDetectionCacheEnabled = oldCacheEnabled
		setting.SensitiveDetectionBreakerThreshold = oldBreakerThreshold
		resetSensitiveDetectionBreakerForTest()
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

// configureSensitiveDetectionForBreakerTest 与默认 fixture 相同，但保留缓存关闭、
// 熔断开启（阈值由调用方设定），便于断言熔断行为。
func configureSensitiveDetectionForBreakerTest(t *testing.T, baseURL string, threshold int) func() {
	t.Helper()
	restore := configureSensitiveDetectionForTest(baseURL)
	oldThreshold := setting.SensitiveDetectionBreakerThreshold
	setting.SensitiveDetectionBreakerThreshold = threshold
	resetSensitiveDetectionBreakerForTest()
	return func() {
		setting.SensitiveDetectionBreakerThreshold = oldThreshold
		resetSensitiveDetectionBreakerForTest()
		restore()
	}
}

func TestSensitiveDetectionBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	// 检测 server 始终返回非 2xx，模拟检测模型故障。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForBreakerTest(t, server.URL, 2)
	defer restore()

	// 前两次调用真实命中故障 server（fail-open 放行，返回 nil）。
	for i := 0; i < 2; i++ {
		c := newSensitiveDetectionTestContext()
		err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, false)
		require.Nil(t, err, "call %d should fail-open", i+1)
	}

	// 第 3 次熔断已打开，应直接放行且不再请求 server。
	c := newSensitiveDetectionTestContext()
	err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, false)
	require.Nil(t, err)
	result, ok := common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusErrorOpen, result.Status)
	assert.Equal(t, "breaker_open", result.Reason)
}

func TestSensitiveDetectionBreakerIgnoresBusinessBlock(t *testing.T) {
	// 检测模型正常返回 status=451（业务拦截），不应被计为熔断失败。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSensitiveDetectionResponse(t, w, `{"status":451}`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForBreakerTest(t, server.URL, 2)
	defer restore()

	// 连续两次业务拦截：应返回拦截错误，且熔断不应打开。
	for i := 0; i < 2; i++ {
		c := newSensitiveDetectionTestContext()
		err := EvaluateSensitiveDetection(c, newSensitiveDetectionOpenAIRequest(), true, false)
		require.NotNil(t, err, "call %d should be blocked", i+1)
		assert.Equal(t, http.StatusForbidden, err.StatusCode)
	}
	// 熔断仍允许调用（业务拦截未触发失败计数）。
	assert.True(t, sensitiveDetectionBreakerAllows(), "business block must not trip the breaker")
}

func TestSensitiveDetectionCacheSkipsModelCallOnHit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeSensitiveDetectionResponse(t, w, `{"status":200}`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForTest(server.URL)
	defer restore()
	// 本用例显式开启缓存（fixture 默认关闭）。
	oldCache := setting.SensitiveDetectionCacheEnabled
	setting.SensitiveDetectionCacheEnabled = true
	defer func() { setting.SensitiveDetectionCacheEnabled = oldCache }()

	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "cached prompt for hit test"}},
	}
	// 第一次调用命中 server。
	c := newSensitiveDetectionTestContext()
	require.Nil(t, EvaluateSensitiveDetection(c, req, true, false))
	assert.Equal(t, 1, callCount)

	// 第二次相同 prompt：应命中缓存，server 不再被调用。
	c = newSensitiveDetectionTestContext()
	require.Nil(t, EvaluateSensitiveDetection(c, req, true, false))
	assert.Equal(t, 1, callCount, "cached call must not hit the detector model")
	result, ok := common.GetContextKeyType[types.SensitiveDetectionResult](c, constant.ContextKeySensitiveDetectionResult)
	require.True(t, ok)
	assert.Equal(t, types.SensitiveDetectionStatusAllowed, result.Status)
	assert.True(t, result.Checked)
}

func TestSensitiveDetectionCacheStoresBlockedResult(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeSensitiveDetectionResponse(t, w, `{"status":451,"reason":"policy"}`)
	}))
	defer server.Close()
	restore := configureSensitiveDetectionForTest(server.URL)
	defer restore()
	oldCache := setting.SensitiveDetectionCacheEnabled
	setting.SensitiveDetectionCacheEnabled = true
	defer func() { setting.SensitiveDetectionCacheEnabled = oldCache }()

	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "blocked cached prompt"}},
	}
	c := newSensitiveDetectionTestContext()
	err := EvaluateSensitiveDetection(c, req, true, false)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.StatusCode)
	assert.Equal(t, 1, callCount)

	// 第二次相同违规 prompt：命中缓存的 blocked 结果，直接拦截，不再调用 server。
	c = newSensitiveDetectionTestContext()
	err = EvaluateSensitiveDetection(c, req, true, false)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.StatusCode)
	assert.Equal(t, 1, callCount, "blocked result should be served from cache")
}

func TestSensitiveDetectionDetectorClientIsIsolated(t *testing.T) {
	// 检测客户端必须拥有独立的 *http.Client 与独立 transport，不复用 relay 转发客户端，
	// 否则高并发下检测压力会反向传染给普通转发。relay 客户端在测试环境未初始化（仅
	// main.go 调用 InitHttpClient），因此这里只断言检测客户端自身非空且 transport 独立。
	InitSensitiveDetectionHttpClient()
	detector := getSensitiveDetectionClient()
	require.NotNil(t, detector)
	require.NotNil(t, detector.Transport, "detector must own its own transport")

	// 再次初始化应返回新实例（独立于上一次），证明它没有退化为复用 relay client。
	InitSensitiveDetectionHttpClient()
	detector2 := getSensitiveDetectionClient()
	require.NotNil(t, detector2)
	assert.NotSame(t, detector, detector2, "Init should construct a fresh dedicated client")
	assert.NotSame(t, detector.Transport, detector2.Transport, "each init must build an isolated pool")
}
