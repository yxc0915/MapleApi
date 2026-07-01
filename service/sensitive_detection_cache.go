package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/hot"
)

// 检测结果缓存：相同 (检测配置 + trigger + 请求文本) 在 TTL 内直接复用，不再调用检测模型。
// 仿 model/subscription.go:86-126 的 HybridCache 单例模式：Redis 优先，未启用时回退内存 LRU。
// 注意：blocked 结果会被缓存——命中即拦截，能在 TTL 内挡住对同一违规 prompt 的重复刷量。
// error_open 结果不缓存（瞬态故障不应被缓存），下次仍会真实调用检测模型。
var (
	sensitiveDetectionCache     *cachex.HybridCache[types.SensitiveDetectionResult]
	sensitiveDetectionCacheOnce sync.Once
)

func getSensitiveDetectionCache() *cachex.HybridCache[types.SensitiveDetectionResult] {
	sensitiveDetectionCacheOnce.Do(func() {
		ttl := time.Duration(sensitiveDetectionCacheTTLSeconds()) * time.Second
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		capacity := setting.SensitiveDetectionCacheMaxItems
		if capacity <= 0 {
			capacity = 2048
		}
		sensitiveDetectionCache = cachex.NewHybridCache[types.SensitiveDetectionResult](cachex.HybridCacheConfig[types.SensitiveDetectionResult]{
			Namespace: cachex.Namespace("new-api:sensitive_detection:v2"),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[types.SensitiveDetectionResult]{},
			Memory: func() *hot.HotCache[string, types.SensitiveDetectionResult] {
				return hot.NewHotCache[string, types.SensitiveDetectionResult](hot.LRU, capacity).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return sensitiveDetectionCache
}

func sensitiveDetectionCacheTTLSeconds() int {
	if setting.SensitiveDetectionCacheTTLSeconds > 0 {
		return setting.SensitiveDetectionCacheTTLSeconds
	}
	return 300
}

// sensitiveDetectionCacheKey 基于检测配置、trigger 与完整请求文本生成稳定的缓存 key。
// 请求文本只参与哈希，不作为明文写入 Redis；不截断文本，避免相同前缀不同后缀串用结果。
func sensitiveDetectionCacheKey(trigger, text string) string {
	fingerprint := strings.Join([]string{
		strings.TrimSpace(setting.SensitiveDetectionModel),
		strings.TrimSpace(setting.SensitiveDetectionBaseURL),
		strings.TrimSpace(setting.SensitiveDetectionPrompt),
	}, "\x1f")
	sum := sha256.Sum256([]byte(fingerprint + "\x1e" + trigger + "\x1f" + sensitiveDetectionNormalizeCacheText(text)))
	return hex.EncodeToString(sum[:])
}

// loadCachedSensitiveDetectionResult 返回缓存的检测结果。
// 返回 found=false 表示未命中或缓存被禁用；返回的 cached 值为空时也表示未命中。
func loadCachedSensitiveDetectionResult(trigger, text string) (types.SensitiveDetectionResult, bool) {
	if !setting.SensitiveDetectionCacheEnabled {
		return types.SensitiveDetectionResult{}, false
	}
	cached, found, err := getSensitiveDetectionCache().Get(sensitiveDetectionCacheKey(trigger, text))
	if err != nil || !found {
		return types.SensitiveDetectionResult{}, false
	}
	return cached, true
}

// storeCachedSensitiveDetectionResult 写入缓存。
// 仅缓存 allowed 与 blocked 结果（确定性的判定），error_open/bypassed 不写。
func storeCachedSensitiveDetectionResult(trigger, text string, result types.SensitiveDetectionResult) {
	if !setting.SensitiveDetectionCacheEnabled {
		return
	}
	if result.Status != types.SensitiveDetectionStatusAllowed &&
		result.Status != types.SensitiveDetectionStatusBlocked {
		return
	}
	ttl := time.Duration(sensitiveDetectionCacheTTLSeconds()) * time.Second
	if err := getSensitiveDetectionCache().SetWithTTL(sensitiveDetectionCacheKey(trigger, text), result, ttl); err != nil {
		// 缓存写入失败不影响主流程，仅记录日志。
		_ = err
	}
}

func sensitiveDetectionNormalizeCacheText(text string) string {
	return strings.TrimSpace(text)
}
