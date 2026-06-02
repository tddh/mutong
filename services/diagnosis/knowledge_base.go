package diagnosis

import (
	"strings"

	"gitee.com/tddh/mutong/models/diagnosis"
)

type KnowledgeBase struct {
	rules []KbRule
}

type KbRule struct {
	ResourceType string
	Keywords     []string
	Suggestion   diagnosis.RemediationSuggestion
}

func NewKnowledgeBase() *KnowledgeBase {
	return &KnowledgeBase{
		rules: []KbRule{
			{
				ResourceType: "Pod",
				Keywords:     []string{"memory", "OOMKilled"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "增加内存限制",
					Description: "Pod 因 OOMKilled 被终止。增加内存 requests/limits 或优化应用内存使用。",
					Steps: []string{
						"检查当前内存使用: kubectl top pod <pod> -n <namespace>",
						"在 Deployment 配置中增加内存 limit",
						"滚动重启: kubectl rollout restart deployment/<name> -n <namespace>",
						"变更后监控内存使用情况",
					},
					RiskLevel:   "low",
					AutoFixable: true,
				},
			},
			{
				ResourceType: "Pod",
				Keywords:     []string{"CPU", "throttling"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "调整 CPU 限制",
					Description: "Pod CPU 被限流。考虑增加 CPU limits 或优化 CPU 密集型操作。",
					Steps: []string{
						"检查 CPU 使用: kubectl top pod <pod> -n <namespace>",
						"在 Deployment 配置中增加 CPU limit",
						"如仅需保证 CPU 请求量，可移除 CPU limits",
					},
					RiskLevel:   "low",
					AutoFixable: true,
				},
			},
			{
				ResourceType: "Pod",
				Keywords:     []string{"CrashLoop", "Restart"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "排查崩溃 Pod",
					Description: "Pod 处于 CrashLoopBackOff 状态。检查日志和事件以定位根因。",
					Steps: []string{
						"查看日志: kubectl logs <pod> -n <namespace> --previous",
						"查看事件: kubectl describe pod <pod> -n <namespace>",
						"确认镜像标签和拉取策略",
						"检查就绪/存活探针配置",
						"确认 ConfigMap/Secret 引用存在",
					},
					RiskLevel:   "medium",
					AutoFixable: false,
				},
			},
			{
				ResourceType: "Pod",
				Keywords:     []string{"Pending"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "解决 Pod 调度问题",
					Description: "Pod 无法调度。检查节点资源、污点和亲和性规则。",
					Steps: []string{
						"查看 Pending 原因: kubectl describe pod <pod> -n <namespace>",
						"确认节点资源: kubectl top nodes",
						"检查节点污点和 Pod 容忍度",
						"检查节点选择器和亲和性规则",
					},
					RiskLevel:   "medium",
					AutoFixable: false,
				},
			},
			{
				ResourceType: "Node",
				Keywords:     []string{"NotReady"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "排查节点健康状态",
					Description: "节点处于 NotReady 状态。检查 kubelet 运行状态、网络连通性和磁盘压力。",
					Steps: []string{
						"SSH 到节点检查 kubelet: systemctl status kubelet",
						"检查磁盘压力: df -h",
						"检查到 API Server 的网络连通性",
						"查看节点状态: kubectl describe node <node>",
						"如不可恢复可考虑驱逐 Pod 后重启节点",
					},
					RiskLevel:   "high",
					AutoFixable: false,
				},
			},
			{
				ResourceType: "Node",
				Keywords:     []string{"DiskPressure", "MemoryPressure"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "缓解节点资源压力",
					Description: "节点资源压力过高。清理无用镜像、日志，或增加节点容量。",
					Steps: []string{
						"清理无用镜像: crictl rmi --prune",
						"清理旧日志: find /var/log -name '*.log' -mtime +7 -delete",
						"检查磁盘占用: du -sh /var/lib/kubelet/*",
						"考虑为集群增加节点容量",
					},
					RiskLevel:   "medium",
					AutoFixable: true,
				},
			},
			{
				ResourceType: "Deployment",
				Keywords:     []string{"issues", "rollout"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "检查 Deployment 发布状态",
					Description: "Deployment 存在异常。检查发布状态和最近变更。",
					Steps: []string{
						"查看发布状态: kubectl rollout status deployment/<name> -n <namespace>",
						"查看发布历史: kubectl rollout history deployment/<name> -n <namespace>",
						"必要时回滚: kubectl rollout undo deployment/<name> -n <namespace>",
						"检查 Pod 事件排查具体故障",
					},
					RiskLevel:   "medium",
					AutoFixable: false,
				},
			},
			{
				ResourceType: "Service",
				Keywords:     []string{"endpoint", "service"},
				Suggestion: diagnosis.RemediationSuggestion{
					Action:      "验证 Service 端点",
					Description: "Service 端点异常。检查后端 Pod 是否健康且匹配选择器。",
					Steps: []string{
						"查看端点: kubectl get endpoints <service> -n <namespace>",
						"确认 Pod 标签匹配 Service selector",
						"检查后端 Pod 是否 Running 且 Ready",
						"测试连通性: kubectl exec -it <pod> -- curl <service>:<port>",
					},
					RiskLevel:   "medium",
					AutoFixable: false,
				},
			},
		},
	}
}

func (kb *KnowledgeBase) GetSuggestions(resourceType string, evidence []string) []diagnosis.RemediationSuggestion {
	var suggestions []diagnosis.RemediationSuggestion

	for _, rule := range kb.rules {
		if rule.ResourceType != resourceType {
			continue
		}

		for _, ev := range evidence {
			evLower := strings.ToLower(ev)
			for _, keyword := range rule.Keywords {
				if strings.Contains(evLower, strings.ToLower(keyword)) {
					suggestions = append(suggestions, rule.Suggestion)
					break
				}
			}
		}
	}

	return suggestions
}
