package diagnosis

import "strings"

// match 用于 Few-shot 示例的匹配结果
type match struct {
	example DiagnosisExample
	score   int
}

// DiagnosisExample 诊断场景的 Few-shot 示例
type DiagnosisExample struct {
	Scenario  string   // 场景名称
	Keywords  []string // 触发关键词
	UserQuery string   // 用户典型提问
	Steps     []string // 推荐工具调用步骤
}

var diagnosisExamples = []DiagnosisExample{
	{
		Scenario:  "Pod CrashLoopBackOff",
		Keywords:  []string{"CrashLoopBackOff", "crashloop", "crash", "重启", "反复重启", "backoff"},
		UserQuery: "production 命名空间 order-service Pod 一直重启，状态 CrashLoopBackOff",
		Steps: []string{
			`1. inspect_resource("Pod", "order-service-xxx", "production")
   → 获取容器状态，查看退出码（ExitCode=137→OOMKilled, 1→应用错误, 0→正常退出）`,
			`2. get_pod_logs("production", "order-service-xxx", tail_lines=100)
   → 获取崩溃前的日志（K8s API 返回时间正序，最近的日志在末尾）`,
			`3. get_resource_metrics("Pod", "order-service-xxx", "production")
   → 查看内存/CPU 使用趋势，判断是否因资源不足被 Kill`,
			`4. query_topology("Pod", "order-service-xxx", "production")
   → 查看上下游依赖，分析影响范围`,
		},
	},
	{
		Scenario:  "Pod OOMKilled",
		Keywords:  []string{"OOMKilled", "oom", "内存溢出", "out of memory", "OOM"},
		UserQuery: "payment-service Pod 被 OOMKilled 了，内存不够用",
		Steps: []string{
			`1. inspect_resource("Pod", "payment-service-xxx", "ns")
   → 确认 ExitCode=137 且 Reason=OOMKilled`,
			`2. get_resource_metrics("Pod", "payment-service-xxx", "ns")
   → 查看内存使用趋势和 limit 值`,
			`3. query_metric_timeseries(expr="container_memory_working_set_bytes{pod='payment-service-xxx'}", start=..., step=300)
   → 查看过去 6 小时内存趋势，确认是突发还是持续增长`,
			`4. get_pod_logs("ns", "payment-service-xxx", tail_lines=200)
   → 查找 OOM 前的内存分配日志`,
		},
	},
	{
		Scenario:  "ImagePullBackOff / ErrImagePull",
		Keywords:  []string{"ImagePullBackOff", "ErrImagePull", "imagepull", "镜像拉取失败", "镜像不存在", "image"},
		UserQuery: "新部署的 api-gateway Pod 状态 ImagePullBackOff，镜像拉不下来",
		Steps: []string{
			`1. inspect_resource("Pod", "api-gateway-xxx", "ns")
   → 查看 containerStatus.waiting.reason 和 message（如 "image not found" 或 "unauthorized"）`,
			`2. 检查镜像名是否正确、imagePullSecrets 是否配置
   → 常见原因：tag 写错、私有仓库未配置 secret、registry 不可达`,
		},
	},
	{
		Scenario:  "NodeNotReady",
		Keywords:  []string{"NodeNotReady", "NotReady", "node not ready", "节点不可用", "node", "节点"},
		UserQuery: "node-3 状态变成 NotReady 了，需要排查原因",
		Steps: []string{
			`1. inspect_resource("Node", "node-3")
   → 查看节点 Conditions（MemoryPressure/DiskPressure/PIDPressure/NetworkUnavailable）`,
			`2. get_resource_metrics("Node", "node-3")
   → 查看节点 CPU/内存/磁盘使用率`,
			`3. list_k8s_resources("Pod", namespace="", limit=100)
   → 确认哪些 Pod 运行在该节点上，检查是否有 Pod 被 Evicted`,
		},
	},
	{
		Scenario:  "告警概览",
		Keywords:  []string{"告警", "alerts", "alert", "报警", "活跃", "最近"},
		UserQuery: "最近有什么告警？帮我看看当前活跃的告警",
		Steps: []string{
			`1. list_alerts(limit=20) 或 get_active_alerts(limit=20)
   → 获取活跃告警列表（含 fingerprint、严重级别、资源、摘要）`,
			`2. 如需深入了解某个告警：get_alert_detail(fingerprint="xxx")
   → 获取告警详情（含资源信息、enrich tags、路由信息）`,
		},
	},
}

// matchExamples 基于用户问题匹配 2 个最相关的 Few-shot 示例
func matchExamples(userQuery string, examples []DiagnosisExample, maxResults int) []DiagnosisExample {
	query := strings.ToLower(userQuery)
	var matches []match

	for _, ex := range examples {
		score := 0
		for _, kw := range ex.Keywords {
			if strings.Contains(query, strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, match{ex, score})
		}
	}

	// 按匹配度排序，取前 maxResults
	sortMatches(matches)
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	var result []DiagnosisExample
	for _, m := range matches {
		result = append(result, m.example)
	}
	return result
}

func sortMatches(matches []match) {
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].score > matches[i].score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

// appendToolExamplesToString 字符串版本（用于 map literal 场景）
func appendToolExamplesToString(systemPrompt string, userQuery string) string {
	var buf strings.Builder
	buf.WriteString(systemPrompt)
	appendToolExamples(&buf, userQuery)
	return buf.String()
}

// GetFirstUserMessage 从聊天历史中提取第一条非空 user 消息
// 第一条 user 消息通常包含最完整的问题上下文
func GetFirstUserMessage(messages []ChatMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Content != "" {
			return msg.Content
		}
	}
	return ""
}

// appendToolExamples 将匹配的 Few-shot 示例追加到 system prompt
func appendToolExamples(buf *strings.Builder, userQuery string) {
	matched := matchExamples(userQuery, diagnosisExamples, 2)
	if len(matched) == 0 {
		return
	}

	buf.WriteString("\n\n## 工具调用示例（供参考）\n")
	for i, ex := range matched {
		buf.WriteString("\n### " + string(rune('A'+i)) + ": " + ex.Scenario + "\n")
		buf.WriteString("用户提问：「" + ex.UserQuery + "」\n\n")
		buf.WriteString("推荐工具调用流程：\n")
		for _, step := range ex.Steps {
			buf.WriteString(step + "\n")
		}
	}
}
