package service

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// 检测客户端拥有独立的连接池，避免与 relay 转发（service.httpClient）共享
// MaxIdleConnsPerHost 而互相挤压——这是高并发下检测压力反向传染给普通转发的根因。
var (
	sensitiveDetectionClient     *http.Client
	sensitiveDetectionClientLock sync.Mutex
)

// InitSensitiveDetectionHttpClient 构造检测专用的 *http.Client。
// 必须在 common.InitEnv() 与 service.InitHttpClient() 之后调用（main.go 启动序列）。
func InitSensitiveDetectionHttpClient() {
	sensitiveDetectionClientLock.Lock()
	defer sensitiveDetectionClientLock.Unlock()
	sensitiveDetectionClient = newSensitiveDetectionHttpClient()
}

func newSensitiveDetectionHttpClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        setting.SensitiveDetectionMaxIdleConns,
		MaxIdleConnsPerHost: setting.SensitiveDetectionMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	// 不设 client.Timeout：单次调用超时由调用处的 context.WithTimeout 控制，
	// 便于运行时通过 SensitiveDetectionTimeoutSeconds 热更新，无需重建 client。
	return &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
}

// getSensitiveDetectionClient 返回检测专用 client。
// 若启动序列尚未初始化，则惰性按当前配置构造一份兜底，确保检测永不因 nil client 失败。
func getSensitiveDetectionClient() *http.Client {
	sensitiveDetectionClientLock.Lock()
	defer sensitiveDetectionClientLock.Unlock()
	if sensitiveDetectionClient == nil {
		sensitiveDetectionClient = newSensitiveDetectionHttpClient()
	}
	return sensitiveDetectionClient
}

// sensitiveDetectionTimeoutDuration 读取运行时配置的单次检测超时。
// 配置为 0 或负数时回退到 5 秒，避免无超时把 goroutine/连接槽长期挂住。
func sensitiveDetectionTimeoutDuration() time.Duration {
	return sensitiveDetectionTimeoutDurationForSeconds(setting.SensitiveDetectionTimeoutSeconds)
}

func sensitiveDetectionTimeoutDurationForSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}
