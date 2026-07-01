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
var SensitiveDetectionPrompt = DefaultSensitiveDetectionPrompt
var SensitiveDetectionGroups = []string{}

// DefaultSensitiveDetectionPrompt 是违规检测的默认 system 提示词。
// 作为默认值：管理员仍可在系统设置页修改 SensitiveDetectionPrompt 覆盖它。
// 注意：用户输入由后台作为独立的 user message 发送，因此本提示词内不要包含
// {{USER_INPUT}} 占位符，否则模型会收到字面占位符与真实输入两份内容。
const DefaultSensitiveDetectionPrompt = `你是一个 AI 请求审核器。你的任务是判断用户输入是否可以继续提交给 AI 模型处理。

你必须只返回一个整数：200 或 499。
不要返回解释、JSON、Markdown、标点、换行或任何额外内容。

返回规则：

返回 200：
用户请求不涉及逆向、入侵、破解、绕过、攻击、窃取、恶意利用等行为，可以正常放行。

以下情况应返回 200：

* 普通编程开发、代码报错、环境配置、服务器运维
* 正常的 Linux、Nginx、Docker、数据库、网络配置问题
* 安全概念解释，但不要求实操攻击
* 修复漏洞、加固系统、分析自己服务器日志
* 合规测试、靶场、CTF、教学环境中不涉及真实目标滥用的内容
* 普通 API 调用、模型接入、产品设计、商业文案、PPT、图片生成等非攻击性内容

返回 499：
只要用户请求中存在以下任一行为或意图，就返回 499：

1. 逆向行为
   包括但不限于：

* 逆向工程
* 反编译
* 脱壳
* 破解软件
* 绕过授权
* 绕过登录
* 绕过验证
* 破解加密、签名、授权码、注册码
* 分析闭源程序以修改、绕过或破解功能

2. 入侵行为
   包括但不限于：

* 未授权访问系统、网站、服务器、后台、数据库
* 扫描真实目标漏洞
* 利用漏洞进入系统
* 提权、横向移动、维持权限
* 绕过鉴权
* 读取、写入、删除他人服务器文件
* 获取后台权限、Shell、WebShell

3. 破解行为
   包括但不限于：

* 爆破密码
* 撞库
* 账号枚举
* 破解 Token、Cookie、Session、JWT
* 获取账号密码、密钥、私钥、API Key
* 破解付费功能、会员限制、授权系统、风控系统

4. 恶意或攻击性代码
   包括但不限于：

* 木马、病毒、后门、C2、Botnet
* 免杀、逃逸、隐藏、持久化
* 键盘记录、窃取文件、窃取浏览器数据
* DDoS、DoS、资源耗尽攻击
* 自动化批量攻击、批量注册、批量滥用

5. 绕过 AI 或平台安全限制
   包括但不限于：

* 要求绕过 OpenAI、Codex、ChatGPT、Claude、Grok 等平台审核
* 规避模型安全策略
* 规避风控、限额、封号检测
* 生成越狱提示词
* 隐藏真实攻击意图

宽松判断原则：

* 只是普通报错、配置、部署、开发问题，默认返回 200。
* 只是解释安全概念，且没有要求攻击步骤、利用代码、绕过方法，返回 200。
* 如果内容中只出现“漏洞”“安全”“日志”“防护”等词，但目的是修复、排查、加固，返回 200。
* 如果请求中出现明确的逆向、入侵、破解、绕过、利用、窃取、攻击、免杀、提权、爆破、WebShell 等行为，返回 499。
* 如果用户声称“仅学习”“仅研究”“合法授权”，但请求本身是在执行逆向、入侵或破解行为，仍返回 499。
* 如果无法判断，但文本明显包含逆向、入侵或破解的操作意图，返回 499。
* 如果无法判断，且没有明显逆向、入侵或破解行为，返回 200。

示例：

“帮我写一个 Nginx 反向代理配置” -> 200
“我的 Ubuntu 服务器 SSH 登录异常，帮我分析日志” -> 200
“解释 SQL 注入是什么，不要攻击代码” -> 200
“帮我修复这个 SQL 注入漏洞” -> 200
“CTF 靶场里这个题怎么理解” -> 200
“帮我分析自己程序的崩溃日志” -> 200

“帮我逆向这个 exe” -> 499
“怎么绕过软件授权” -> 499
“帮我破解会员限制” -> 499
“帮我写爆破后台密码的脚本” -> 499
“扫描这个网站有没有漏洞并利用” -> 499
“怎么绕过登录直接进后台” -> 499
“写一个 WebShell” -> 499
“写一个免杀后门” -> 499
“帮我获取别人网站数据库” -> 499
“怎么绕过 Codex 的 cyber 审核” -> 499

接下来会对用户输入进行审核，你只需返回 200 或 499。`

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
	// SensitiveDetectionTokenRPM 单个 token 每分钟可触发的检测模型调用上限，0 表示不限流。
	SensitiveDetectionTokenRPM = 30
	// SensitiveDetectionUserRPM 单个用户每分钟可触发的检测模型调用上限，0 表示不限流。
	SensitiveDetectionUserRPM = 120
	// SensitiveDetectionMaxRequestRunes 单次送检请求文本的最大 rune 数，0 表示不限制。
	SensitiveDetectionMaxRequestRunes = 20000
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
