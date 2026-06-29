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

// 违规检测性能参数。0 表示无限制/禁用（按字段语义）。
// 这些值支持运行时通过系统设置热更新（见 model/option.go updateOptionMap）。
var (
	// SensitiveDetectionTimeoutSeconds 单次检测调用的最大等待时长（秒）。0 视为 5。
	SensitiveDetectionTimeoutSeconds = 5
	// SensitiveDetectionMaxIdleConns 检测客户端独立连接池的全局空闲连接上限。
	SensitiveDetectionMaxIdleConns = 256
	// SensitiveDetectionMaxIdleConnsPerHost 检测客户端独立连接池的单 host 空闲连接上限。
	SensitiveDetectionMaxIdleConnsPerHost = 128
	// SensitiveDetectionRPM 发往检测模型的每分钟调用上限，0 表示不限流。
	SensitiveDetectionRPM = 0
	// SensitiveDetectionCacheEnabled 是否缓存检测结果（命中即复用，不再调用检测模型）。
	SensitiveDetectionCacheEnabled = true
	// SensitiveDetectionCacheTTLSeconds 缓存条目存活时长（秒）。
	SensitiveDetectionCacheTTLSeconds = 300
	// SensitiveDetectionCacheMaxItems Redis 不可用时内存回退 LRU 的容量。
	SensitiveDetectionCacheMaxItems = 2048
	// SensitiveDetectionBreakerThreshold 连续失败几次后触发熔断，0 表示不熔断。
	SensitiveDetectionBreakerThreshold = 5
	// SensitiveDetectionBreakerCooldownSeconds 熔断打开后的冷却时长（秒）。
	SensitiveDetectionBreakerCooldownSeconds = 30
)

// SensitiveDetectionCacheEnabledBool 以 bool 形式返回缓存开关，避免调用方重复解析。
func SensitiveDetectionCacheEnabledBool() bool {
	return SensitiveDetectionCacheEnabled
}

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
