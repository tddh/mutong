package diagnosis

import (
	"strings"

	"gitee.com/tddh/mutong/models/diagnosis"
)

// DiagnosisScene 诊断场景定义（关键词全部来自 kubernetes 社区工具源码）
type DiagnosisScene struct {
	Name     string   // 场景名称
	Keywords []string // 触发关键词（来源：k9s pod.go + kubernetes-mixin alerts + kubelet/events）
	Tools    []string // 该场景需要的工具名列表
}

var DiagnosisScenes = []DiagnosisScene{
	{
		Name: "pod_troubleshoot",
		Keywords: []string{
			"CrashLoopBackOff", "crashloop", "OOMKilled", "oom",
			"ImagePullBackOff", "imagepull", "ErrImagePull",
			"CreateContainerConfigError", "Evicted", "Pending",
			"Error:", "Terminating", "Unknown", "BackOff",
			"Unhealthy", "KubePodCrashLooping", "KubePodNotReady",
			"KubeContainerWaiting", "PodInitializing",
			"ContainerStatusUnknown", "SchedulingGated",
			"重启", "崩溃", "内存", "cpu", "退出码",
			"pod", "不健康", "报错", "异常", "检查", "日志", "状态",
			"修复", "自愈", "处置",
		},
		Tools: []string{
			"inspect_resource", "get_pod_logs", "get_pod_logs_es",
			"get_resource_metrics", "query_topology",
			"search_logs", "get_error_logs",
			"search_similar_cases", "query_metric_timeseries",
			"list_resources_from_cache", "list_k8s_resources",
			"list_alerts", "get_active_alerts", "get_alert_detail",
			"run_diagnosis",
			"restart_pod_safe", "rollout_restart", "scale_deployment",
			"update_deployment_image", "adjust_resource_limits",
		},
	},
	{
		Name: "node_troubleshoot",
		Keywords: []string{
			"NodeNotReady", "NotReady", "DiskPressure", "MemoryPressure",
			"PIDPressure", "NetworkUnavailable", "KubeletSetupFailed",
			"NodeLost", "Rebooted", "Shutdown", "NodeSchedulable",
			"NodeNotSchedulable", "node", "节点", "不可用", "磁盘", "磁盘压力", "not ready",
		},
		Tools: []string{
			"inspect_resource", "get_resource_metrics",
			"list_k8s_resources", "list_resources_from_cache",
			"query_topology",
		},
	},
	{
		Name: "alert_overview",
		Keywords: []string{
			"告警", "alerts", "alert", "报警", "活跃", "最近",
			"KubePod", "KubeDeployment", "KubeNode", "alertname",
		},
		Tools: []string{
			"list_alerts", "get_active_alerts", "get_alert_detail", "run_diagnosis",
		},
	},
	{
		Name: "system_health",
		Keywords: []string{
			"health", "巡检", "inspection", "系统状态", "status",
			"healthz", "ready", "正常", "健康检查",
		},
		Tools: []string{
			"get_system_health", "get_inspection_report", "list_alerts",
		},
	},
	{
		Name: "deployment_troubleshoot",
		Keywords: []string{
			"Deployment", "deployment", "StatefulSet", "statefulset",
			"DaemonSet", "daemonset", "rollout", "stuck",
			"replicas", "mismatch", "KubeDeploymentRolloutStuck",
			"KubeStatefulSetUpdateNotRolledOut",
			"KubeDaemonSetNotScheduled", "FailedScheduling",
			"FailedCreate", "副本", "滚动", "扩容", "发布", "更新",
		},
		Tools: []string{
			"inspect_resource", "list_k8s_resources",
			"get_resource_metrics", "query_topology", "get_pod_logs",
			"rollout_restart", "scale_deployment",
			"update_deployment_image", "adjust_resource_limits",
		},
	},
	{
		Name: "metric_analysis",
		Keywords: []string{
			"指标", "metric", "metrics", "promql", "PromQL",
			"查询", "query", "趋势", "timeseries", "KubeHpa", "监控", "查看",
		},
		Tools: []string{
			"get_metric_catalog", "query_metric_timeseries", "get_resource_metrics",
		},
	},
}

// SelectTools 基于用户问题关键词过滤工具列表
func SelectTools(scenes []DiagnosisScene, userQuery string, allToolDefs []diagnosis.ToolDefinition) []diagnosis.ToolDefinition {
	// 归一化：去空格后转小写，使 "not ready" 能匹配 "NotReady"
	normalizedQuery := strings.ToLower(strings.ReplaceAll(userQuery, " ", ""))
	var matchedScenes []DiagnosisScene

	for _, scene := range scenes {
		for _, kw := range scene.Keywords {
			normalizedKW := strings.ToLower(strings.ReplaceAll(kw, " ", ""))
			if strings.Contains(normalizedQuery, normalizedKW) {
				matchedScenes = append(matchedScenes, scene)
				break
			}
		}
	}

	if len(matchedScenes) == 0 {
		return allToolDefs
	}

	toolSet := make(map[string]bool)
	for _, scene := range matchedScenes {
		for _, t := range scene.Tools {
			toolSet[t] = true
		}
	}

	var filtered []diagnosis.ToolDefinition
	for _, def := range allToolDefs {
		if toolSet[def.Name] {
			filtered = append(filtered, def)
		}
	}
	return filtered
}
