package model

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveDetectionAuditRecordsRawRequestAndMetadata(t *testing.T) {
	truncateTables(t)

	rawBody := `{"model":"gpt-test","messages":[{"role":"user","content":"full request body"}]}`
	c := newSensitiveDetectionAuditTestContext(rawBody)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer should-not-be-stored")
	c.Request.Header.Set("Cookie", "session=should-not-be-stored")
	c.Set(common.RequestIdKey, "req-audit-1")
	c.Set("token_name", "audit-token")
	common.SetContextKey(c, constant.ContextKeyUserId, 12)
	common.SetContextKey(c, constant.ContextKeyTokenId, 34)
	common.SetContextKey(c, constant.ContextKeyChannelId, 56)
	common.SetContextKey(c, constant.ContextKeySensitiveDetectionGroup, "vip")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-test")

	result := types.SensitiveDetectionResult{
		Status:         types.SensitiveDetectionStatusFlagged,
		Checked:        true,
		Trigger:        "post:channel",
		Objects:        `["policy"]`,
		Reason:         "policy hit",
		DetectorStatus: 499,
	}
	audit, err := RecordSensitiveDetectionAudit(c, result, "model output preview")
	require.NoError(t, err)
	require.NotNil(t, audit)

	reloaded, err := GetSensitiveDetectionAuditByID(audit.Id)
	require.NoError(t, err)
	bodyHash := sha256.Sum256([]byte(rawBody))
	assert.Equal(t, "req-audit-1", reloaded.RequestId)
	assert.Equal(t, 12, reloaded.UserId)
	assert.Equal(t, 34, reloaded.TokenId)
	assert.Equal(t, "audit-token", reloaded.TokenName)
	assert.Equal(t, 56, reloaded.ChannelId)
	assert.Equal(t, "vip", reloaded.GroupName)
	assert.Equal(t, "gpt-test", reloaded.ModelName)
	assert.Equal(t, string(types.SensitiveDetectionStatusFlagged), reloaded.Status)
	assert.Equal(t, "post:channel", reloaded.Trigger)
	assert.Equal(t, 499, reloaded.DetectorStatus)
	assert.Equal(t, http.MethodPost, reloaded.Method)
	assert.Equal(t, "/v1/chat/completions", reloaded.Path)
	assert.Equal(t, "x=1", reloaded.Query)
	assert.Equal(t, "application/json", reloaded.ContentType)
	assert.Equal(t, rawBody, reloaded.RequestBody)
	assert.Equal(t, int64(len(rawBody)), reloaded.RequestBodyBytes)
	assert.Equal(t, hex.EncodeToString(bodyHash[:]), reloaded.RequestBodySHA256)
	assert.Equal(t, "model output preview", reloaded.ResponseText)
	assert.NotContains(t, reloaded.RequestBody, "should-not-be-stored")
}

func TestFormatUserLogsHidesSensitiveDetectionAuditReferences(t *testing.T) {
	logs := []*Log{
		{
			Id:                               100,
			ChannelName:                      "private-channel",
			SensitiveDetectionStatus:         string(types.SensitiveDetectionStatusBlocked),
			SensitiveDetectionChecked:        true,
			SensitiveDetectionTrigger:        "post:channel",
			SensitiveDetectionObjects:        `["policy"]`,
			SensitiveDetectionReason:         "policy hit",
			SensitiveDetectionDetectorStatus: 499,
			Other: common.MapToJsonStr(map[string]interface{}{
				"sensitive_detection_audit_id": 12,
				"request_body_sha256":          "abcdef",
				"request_body_bytes":           128,
				"visible":                      "kept",
			}),
		},
	}

	formatUserLogs(logs, 0)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Empty(t, logs[0].ChannelName)
	assert.Equal(t, 1, logs[0].Id)
	assert.Empty(t, logs[0].SensitiveDetectionStatus)
	assert.False(t, logs[0].SensitiveDetectionChecked)
	assert.Empty(t, logs[0].SensitiveDetectionTrigger)
	assert.Empty(t, logs[0].SensitiveDetectionObjects)
	assert.Empty(t, logs[0].SensitiveDetectionReason)
	assert.Zero(t, logs[0].SensitiveDetectionDetectorStatus)
	assert.Equal(t, "kept", other["visible"])
	assert.NotContains(t, other, "sensitive_detection_audit_id")
	assert.NotContains(t, other, "request_body_sha256")
	assert.NotContains(t, other, "request_body_bytes")
}

func TestGetUserLogsHidesSensitiveDetectionFields(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]*Log{
		{
			UserId:                           1,
			Username:                         "demo-user",
			CreatedAt:                        100,
			Type:                             LogTypeConsume,
			Content:                          "allowed request",
			SensitiveDetectionStatus:         string(types.SensitiveDetectionStatusAllowed),
			SensitiveDetectionChecked:        true,
			SensitiveDetectionTrigger:        "pre:channel",
			SensitiveDetectionObjects:        `[]`,
			SensitiveDetectionReason:         "ok",
			SensitiveDetectionDetectorStatus: 200,
		},
		{
			UserId:                           1,
			Username:                         "demo-user",
			CreatedAt:                        101,
			Type:                             LogTypeConsume,
			Content:                          "blocked request",
			SensitiveDetectionStatus:         string(types.SensitiveDetectionStatusBlocked),
			SensitiveDetectionChecked:        true,
			SensitiveDetectionTrigger:        "post:channel",
			SensitiveDetectionObjects:        `["policy"]`,
			SensitiveDetectionReason:         "policy hit",
			SensitiveDetectionDetectorStatus: 499,
		},
	}).Error)

	logs, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 2)
	assert.Equal(t, int64(2), total)
	for _, log := range logs {
		assert.Empty(t, log.SensitiveDetectionStatus)
		assert.False(t, log.SensitiveDetectionChecked)
		assert.Empty(t, log.SensitiveDetectionTrigger)
		assert.Empty(t, log.SensitiveDetectionObjects)
		assert.Empty(t, log.SensitiveDetectionReason)
		assert.Zero(t, log.SensitiveDetectionDetectorStatus)
	}
}

func newSensitiveDetectionAuditTestContext(rawBody string) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?x=1", strings.NewReader(rawBody))
	return c
}
