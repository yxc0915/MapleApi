package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// var SensitiveWords []string
var SensitiveWords = []string{
	"test_sensitive",
}

var SensitiveDetectionModel = ""
var SensitiveDetectionBaseURL = ""
var SensitiveDetectionAPIKey = ""
var SensitiveDetectionPrompt = ""
var SensitiveDetectionGroups = []string{}

func SensitiveWordsToString() string {
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	SensitiveWords = []string{}
	sw := strings.Split(s, "\n")
	for _, w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			SensitiveWords = append(SensitiveWords, w)
		}
	}
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

func SensitiveDetectionGroups2JSONString() string {
	jsonBytes, err := common.Marshal(SensitiveDetectionGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func UpdateSensitiveDetectionGroupsByJSONString(jsonStr string) error {
	if strings.TrimSpace(jsonStr) == "" {
		SensitiveDetectionGroups = []string{}
		return nil
	}
	groups := make([]string, 0)
	if err := common.UnmarshalJsonStr(jsonStr, &groups); err != nil {
		return err
	}
	SensitiveDetectionGroups = make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			SensitiveDetectionGroups = append(SensitiveDetectionGroups, group)
		}
	}
	return nil
}

func SensitiveDetectionGroupEnabled(group string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	for _, enabledGroup := range SensitiveDetectionGroups {
		if enabledGroup == group {
			return true
		}
	}
	return false
}

func SensitiveDetectionConfigured() bool {
	return strings.TrimSpace(SensitiveDetectionModel) != "" &&
		strings.TrimSpace(SensitiveDetectionBaseURL) != "" &&
		strings.TrimSpace(SensitiveDetectionAPIKey) != "" &&
		strings.TrimSpace(SensitiveDetectionPrompt) != ""
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
