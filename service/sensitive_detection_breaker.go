package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting"
)

// sensitiveBreaker 是违规检测的纯内存熔断器。
// 仅对“检测调用本身失败”（网络错误 / 超时 / 非 2xx HTTP 响应）计数；
// 检测模型正常返回 status!=200（业务拦截）不算失败——那是模型在正常工作。
// 熔断打开期间直接 fail-open 放行，不调用检测模型，避免检测故障把全站拖死。
//
// 多实例下每个实例各自累计失败次数，判定会略松（每个实例都要累计到阈值才熔断）。
// 这是有意为之：宁可多检测也不漏检。
type sensitiveBreaker struct {
	mu              sync.Mutex
	consecutiveFail int
	openUntil       time.Time
}

var sensitiveDetectionBreaker sensitiveBreaker

// sensitiveDetectionBreakerAllows 返回 true 表示当前允许调用检测模型。
// 当熔断打开（now < openUntil）时返回 false，调用方应直接 fail-open 放行。
// 阈值配置为 0 时熔断被禁用，永远允许。
func sensitiveDetectionBreakerAllows() bool {
	if setting.SensitiveDetectionBreakerThreshold <= 0 {
		return true
	}
	sensitiveDetectionBreaker.mu.Lock()
	defer sensitiveDetectionBreaker.mu.Unlock()
	return !time.Now().Before(sensitiveDetectionBreaker.openUntil)
}

// recordSensitiveDetectionCallOutcome 在每次检测调用返回后更新熔断状态。
// success=true（正常拿到 status JSON）重置失败计数；
// success=false（调用失败）递增失败计数，达到阈值则打开熔断一段时间。
// 熔断打开后，半开探测靠冷却到期自然完成：冷却一过即允许下一次真实调用，
// 若该调用成功则重置计数、若失败则重新打开。
func recordSensitiveDetectionCallOutcome(success bool) {
	if setting.SensitiveDetectionBreakerThreshold <= 0 {
		return
	}
	cooldown := setting.SensitiveDetectionBreakerCooldownSeconds
	if cooldown <= 0 {
		cooldown = 30
	}
	sensitiveDetectionBreaker.mu.Lock()
	defer sensitiveDetectionBreaker.mu.Unlock()
	if success {
		sensitiveDetectionBreaker.consecutiveFail = 0
		sensitiveDetectionBreaker.openUntil = time.Time{}
		return
	}
	sensitiveDetectionBreaker.consecutiveFail++
	if sensitiveDetectionBreaker.consecutiveFail >= setting.SensitiveDetectionBreakerThreshold {
		sensitiveDetectionBreaker.openUntil = time.Now().Add(time.Duration(cooldown) * time.Second)
	}
}

// resetSensitiveDetectionBreakerForTest 仅供测试重置全局熔断状态。
func resetSensitiveDetectionBreakerForTest() {
	sensitiveDetectionBreaker.mu.Lock()
	defer sensitiveDetectionBreaker.mu.Unlock()
	sensitiveDetectionBreaker.consecutiveFail = 0
	sensitiveDetectionBreaker.openUntil = time.Time{}
}
