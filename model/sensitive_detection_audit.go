package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const sensitiveDetectionAuditResponsePreviewRunes = 4096

type SensitiveDetectionAudit struct {
	Id        int   `json:"id" gorm:"primaryKey"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`

	RequestId string `json:"request_id" gorm:"size:64;index;default:''"`
	UserId    int    `json:"user_id" gorm:"index;default:0"`
	TokenId   int    `json:"token_id" gorm:"index;default:0"`
	TokenName string `json:"token_name" gorm:"size:191;index;default:''"`
	ChannelId int    `json:"channel_id" gorm:"index;default:0"`
	GroupName string `json:"group" gorm:"column:group_name;size:191;index;default:''"`
	ModelName string `json:"model_name" gorm:"size:191;index;default:''"`

	Status         string `json:"status" gorm:"size:32;index;default:''"`
	Trigger        string `json:"trigger" gorm:"size:32;index;default:''"`
	Objects        string `json:"objects" gorm:"type:text"`
	Reason         string `json:"reason" gorm:"type:text"`
	DetectorStatus int    `json:"detector_status" gorm:"default:0"`

	Method            string `json:"method" gorm:"size:16;default:''"`
	Path              string `json:"path" gorm:"type:text"`
	Query             string `json:"query" gorm:"type:text"`
	ContentType       string `json:"content_type" gorm:"type:text"`
	RequestBody       string `json:"request_body"`
	RequestBodyBytes  int64  `json:"request_body_bytes" gorm:"default:0"`
	RequestBodySHA256 string `json:"request_body_sha256" gorm:"size:64;index;default:''"`
	ResponseText      string `json:"response_text_preview" gorm:"column:response_text_preview;type:text"`
}

func RecordSensitiveDetectionAudit(c *gin.Context, result types.SensitiveDetectionResult, responseText string) (*SensitiveDetectionAudit, error) {
	if DB == nil {
		return nil, nil
	}
	var body []byte
	var bodyBytes int64
	if c != nil {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, err
		}
		body, err = storage.Bytes()
		if err != nil {
			return nil, err
		}
		bodyBytes = storage.Size()
	}
	bodyHash := sha256.Sum256(body)
	audit := &SensitiveDetectionAudit{
		CreatedAt:         common.GetTimestamp(),
		RequestId:         sensitiveDetectionAuditRequestID(c),
		UserId:            common.GetContextKeyInt(c, constant.ContextKeyUserId),
		TokenId:           common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		TokenName:         c.GetString("token_name"),
		ChannelId:         common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		GroupName:         common.GetContextKeyString(c, constant.ContextKeySensitiveDetectionGroup),
		ModelName:         common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
		Status:            string(result.Status),
		Trigger:           result.Trigger,
		Objects:           result.Objects,
		Reason:            result.Reason,
		DetectorStatus:    result.DetectorStatus,
		Method:            sensitiveDetectionAuditMethod(c),
		Path:              sensitiveDetectionAuditPath(c),
		Query:             sensitiveDetectionAuditQuery(c),
		ContentType:       sensitiveDetectionAuditContentType(c),
		RequestBody:       string(body),
		RequestBodyBytes:  bodyBytes,
		RequestBodySHA256: hex.EncodeToString(bodyHash[:]),
		ResponseText:      truncateSensitiveDetectionAuditText(responseText, sensitiveDetectionAuditResponsePreviewRunes),
	}
	if audit.GroupName == "" {
		audit.GroupName = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	if audit.ModelName == "" {
		audit.ModelName = c.GetString("model_name")
	}
	if err := DB.Create(audit).Error; err != nil {
		return nil, err
	}
	return audit, nil
}

func GetSensitiveDetectionAuditByID(id int) (*SensitiveDetectionAudit, error) {
	var audit SensitiveDetectionAudit
	if err := DB.Where("id = ?", id).First(&audit).Error; err != nil {
		return nil, err
	}
	return &audit, nil
}

func sensitiveDetectionAuditRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if requestID := c.GetString(common.RequestIdKey); requestID != "" {
		return requestID
	}
	if c.Request != nil {
		return c.Request.Header.Get(common.RequestIdKey)
	}
	return ""
}

func sensitiveDetectionAuditMethod(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return c.Request.Method
}

func sensitiveDetectionAuditPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func sensitiveDetectionAuditQuery(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.RawQuery
}

func sensitiveDetectionAuditContentType(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return c.Request.Header.Get("Content-Type")
}

func truncateSensitiveDetectionAuditText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}
