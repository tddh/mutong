package retrospective

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
	"gitee.com/tddh/mutong/models/retrospective"
	diagnosis_svc "gitee.com/tddh/mutong/services/diagnosis"
)

type DiagnosisCache interface {
	GetCachedDiagnosisResult(ctx context.Context, fingerprint string) (*diagnosis.DiagnosisResult, error)
}

type Service struct {
	logger          interfaces.Logger
	graphDB         interfaces.GraphDB
	alertStorage    alert_interfaces.AlertProcessor
	db              *gorm.DB
	llmProvider     interfaces.LLMProvider
	diagCache       DiagnosisCache
	hybridRetriever *diagnosis_svc.HybridRetriever
	executor        interfaces.Executor
}

func NewService(logger interfaces.Logger, graphDB interfaces.GraphDB, alertStorage alert_interfaces.AlertProcessor, db *gorm.DB, llmProvider interfaces.LLMProvider, diagCache DiagnosisCache) *Service {
	return &Service{
		logger:       logger,
		graphDB:      graphDB,
		alertStorage: alertStorage,
		db:           db,
		llmProvider:  llmProvider,
		diagCache:    diagCache,
	}
}

func (s *Service) BuildTimeline(ctx context.Context, fingerprint string) ([]retrospective.TimelineEvent, error) {
	alert, err := s.alertStorage.GetAlertByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	var events []retrospective.TimelineEvent

	events = append(events, retrospective.TimelineEvent{
		Timestamp:   alert.StartsAt,
		EventType:   "alert_triggered",
		Description: fmt.Sprintf("告警 %s 在 %s/%s 触发", alert.Labels["alertname"], alert.ResourceType, alert.ResourceName),
		Source:      "prometheus",
		Severity:    alert.Routing.Severity,
		Resource:    fmt.Sprintf("%s/%s", alert.ResourceType, alert.ResourceName),
	})

	filters := map[string]string{}
	if alert.Namespace != "" {
		filters["namespace"] = alert.Namespace
	}
	if alert.NodeName != "" {
		filters["nodeName"] = alert.NodeName
	}

	relatedAlerts, err := s.alertStorage.GetActiveAlerts(ctx, filters)
	if err == nil {
		for _, ra := range relatedAlerts {
			if ra.Fingerprint == fingerprint {
				continue
			}
			events = append(events, retrospective.TimelineEvent{
				Timestamp:   ra.StartsAt,
				EventType:   "related_alert",
				Description: fmt.Sprintf("关联告警 %s 在 %s/%s 触发", ra.Labels["alertname"], ra.ResourceType, ra.ResourceName),
				Source:      "prometheus",
				Severity:    ra.Routing.Severity,
				Resource:    fmt.Sprintf("%s/%s", ra.ResourceType, ra.ResourceName),
			})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events, nil
}

func (s *Service) AnalyzeCausalChain(ctx context.Context, fingerprint string) (*retrospective.CausalChain, error) {
	alert, err := s.alertStorage.GetAlertByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	chain := &retrospective.CausalChain{
		RootCause: fmt.Sprintf("%s/%s", alert.ResourceType, alert.ResourceName),
	}

	filters := map[string]string{}
	if alert.Namespace != "" {
		filters["namespace"] = alert.Namespace
	}
	if alert.NodeName != "" {
		filters["nodeName"] = alert.NodeName
	}

	relatedAlerts, err := s.alertStorage.GetActiveAlerts(ctx, filters)
	if err != nil {
		return chain, nil
	}

	var nodeAlerts, podAlerts []*alert_models.ProcessedAlert
	for _, ra := range relatedAlerts {
		if ra.ResourceType == "Node" {
			nodeAlerts = append(nodeAlerts, ra)
		} else {
			podAlerts = append(podAlerts, ra)
		}
	}

	if len(nodeAlerts) > 0 {
		for _, na := range nodeAlerts {
			chain.Links = append(chain.Links, retrospective.CausalLink{
				Cause:      fmt.Sprintf("Node %s: %s", na.ResourceName, na.Labels["alertname"]),
				Effect:     fmt.Sprintf("%s 上的 Pod 故障", na.ResourceName),
				Confidence: 0.85,
				Evidence:   []string{fmt.Sprintf("节点告警于 %s 开始", na.StartsAt.Format(time.RFC3339))},
			})
		}
		chain.RootCause = fmt.Sprintf("Node/%s", nodeAlerts[0].ResourceName)
	}

	for _, pa := range podAlerts {
		chain.Links = append(chain.Links, retrospective.CausalLink{
			Cause:      fmt.Sprintf("Pod %s: %s", pa.ResourceName, pa.Labels["alertname"]),
			Effect:     fmt.Sprintf("%s 服务降级", pa.ResourceName),
			Confidence: 0.6,
			Evidence:   []string{fmt.Sprintf("Pod 告警于 %s 触发", pa.StartsAt.Format(time.RFC3339))},
		})
	}

	if len(chain.Links) == 0 {
		chain.Links = append(chain.Links, retrospective.CausalLink{
			Cause:      fmt.Sprintf("%s %s: %s", alert.ResourceType, alert.ResourceName, alert.Labels["alertname"]),
			Effect:     "服务影响",
			Confidence: 0.5,
			Evidence:   []string{"单条告警，无关联事件"},
		})
	}

	chain.Impact = fmt.Sprintf("发现 %d 条关联告警", len(relatedAlerts))

	return chain, nil
}

func (s *Service) GeneratePostmortem(ctx context.Context, fingerprint string) (*retrospective.PostmortemReport, error) {
	alert, err := s.alertStorage.GetAlertByFingerprint(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	timeline, err := s.BuildTimeline(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to build timeline: %w", err)
	}

	causalChain, err := s.AnalyzeCausalChain(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze causal chain: %w", err)
	}

	report := &retrospective.PostmortemReport{
		ID:            uuid.New().String(),
		IncidentTitle: fmt.Sprintf("%s on %s/%s", alert.Labels["alertname"], alert.ResourceType, alert.ResourceName),
		StartTime:     alert.StartsAt,
		EndTime:       time.Now(),
		Severity:      alert.Routing.Severity,
		Timeline:      timeline,
		CausalChain:   *causalChain,
		RootCause:     retrospective.EditableField{AIGenerated: causalChain.RootCause, Final: causalChain.RootCause},
		GeneratedAt:   time.Now(),
	}

	if !alert.EndsAt.IsZero() {
		report.EndTime = alert.EndsAt
	}
	report.Duration = report.EndTime.Sub(report.StartTime).String()

	// 计算检测方式与 MTTD（平均检测时间）
	report.DetectionMethod = "monitor"
	if alert.Labels != nil {
		if source, ok := alert.Labels["source"]; ok {
			report.DetectionMethod = source
		}
	}
	if len(report.Timeline) > 0 {
		detectDuration := report.Timeline[0].Timestamp.Sub(report.StartTime)
		if detectDuration > 0 {
			report.MTTD = detectDuration.Round(time.Second).String()
		} else {
			report.MTTD = "< 1s"
		}
	}

	seen := make(map[string]bool)
	for _, link := range causalChain.Links {
		svc := link.Effect
		if !seen[svc] {
			seen[svc] = true
			report.ImpactedSvc = append(report.ImpactedSvc, svc)
		}
	}

	if alert.BusinessContext.AppName != "" {
		report.ImpactedSvc = append(report.ImpactedSvc,
			fmt.Sprintf("业务应用: %s (团队: %s, 关键度: %s)",
				alert.BusinessContext.AppName,
				alert.BusinessContext.Team,
				alert.BusinessContext.Criticality))
	}

	report.LessonsLearned = s.generateLessonsLearned(alert, causalChain)
	report.ActionItems = s.generateActionItems(alert, causalChain)
	report.Resolution = retrospective.EditableField{AIGenerated: s.generateResolution(alert), Final: s.generateResolution(alert)}

	s.enrichFromDiagnosisCache(report, fingerprint)

	if alert.BusinessContext.AppName != "" {
		report.BusinessContext = &retrospective.BusinessReportContext{
			AppName:      alert.BusinessContext.AppName,
			Team:         alert.BusinessContext.Team,
			Criticality:  alert.BusinessContext.Criticality,
			BusinessUnit: alert.BusinessContext.BusinessUnit,
			Environment:  alert.BusinessContext.Environment,
		}
	}

	// 如果诊断缓存中的 BusinessCalls 缺少上下游数据，回退到告警原始数据
	bizCallsEmpty := report.BusinessCalls == nil || (len(report.BusinessCalls.Upstreams) == 0 && len(report.BusinessCalls.Downstreams) == 0)
	if bizCallsEmpty && alert.BusinessCalls != nil {
		var upstreams, downstreams []retrospective.BusinessAppRef
		for _, u := range alert.BusinessCalls.Upstreams {
			upstreams = append(upstreams, retrospective.BusinessAppRef{
				AppName: u.AppName, Team: u.Team, Criticality: u.Criticality,
			})
		}
		for _, d := range alert.BusinessCalls.Downstreams {
			downstreams = append(downstreams, retrospective.BusinessAppRef{
				AppName: d.AppName, Team: d.Team, Criticality: d.Criticality,
			})
		}
		report.BusinessCalls = &retrospective.BusinessReportCalls{
			AppName:     alert.BusinessCalls.AppName,
			Upstreams:   upstreams,
			Downstreams: downstreams,
		}
	}

	if report.BusinessCalls == nil && alert.BusinessContext.AppName != "" {
		s.enrichBusinessCallsFromNebula(report, alert.BusinessContext.AppName, alert.BusinessContext.Namespace)
	}

	if alert.ResourceType == "Pod" && alert.ResourceName != "" {
		s.enrichWorkloadContext(report, alert.ResourceName, alert.Namespace)
	}

	s.enrichFromExecutionRecords(ctx, report, fingerprint)

	if s.llmProvider != nil {
		s.enrichWithLLM(report, alert, fingerprint)
	}

	if s.db != nil {
		go s.savePostmortem(report, fingerprint, alert)
	}

	return report, nil
}

func (s *Service) generateLessonsLearned(alert *alert_models.ProcessedAlert, chain *retrospective.CausalChain) []string {
	var lessons []string

	if strings.Contains(alert.Labels["alertname"], "NotReady") {
		lessons = append(lessons, "节点健康监控应包含早期预警指标")
		lessons = append(lessons, "建议实现节点健康降级时自动驱逐")
	}

	if strings.Contains(alert.Labels["alertname"], "OOM") || strings.Contains(alert.Labels["alertname"], "Memory") {
		lessons = append(lessons, "资源限制应基于实际使用模式设置")
		lessons = append(lessons, "建议启用 VPA 实现动态资源调整")
	}

	if len(chain.Links) > 2 {
		lessons = append(lessons, "检测到级联故障 — 审查关键组件的爆炸半径")
		lessons = append(lessons, "建议引入熔断机制以遏制故障传播")
	}

	if len(lessons) == 0 {
		lessons = append(lessons, "检查监控覆盖范围以实现早期发现")
		lessons = append(lessons, "编写此类告警的应急响应流程文档")
	}

	return lessons
}

func (s *Service) generateActionItems(alert *alert_models.ProcessedAlert, chain *retrospective.CausalChain) []retrospective.ActionItem {
	var items []retrospective.ActionItem

	items = append(items, retrospective.ActionItem{
		Description: fmt.Sprintf("排查根因: %s", chain.RootCause),
		Owner:       "值班工程师",
		Priority:    "high",
		DueDate:     time.Now().Add(24 * time.Hour).Format("2006-01-02"),
		Status:      "open",
	})

	items = append(items, retrospective.ActionItem{
		Description: "根据故障发现更新运维手册",
		Owner:       "SRE 团队",
		Priority:    "medium",
		DueDate:     time.Now().Add(72 * time.Hour).Format("2006-01-02"),
		Status:      "open",
	})

	items = append(items, retrospective.ActionItem{
		Description: "检查并更新告警阈值",
		Owner:       "监控团队",
		Priority:    "medium",
		DueDate:     time.Now().Add(7 * 24 * time.Hour).Format("2006-01-02"),
		Status:      "open",
	})

	return items
}

func (s *Service) generateResolution(alert *alert_models.ProcessedAlert) string {
	if alert.Status == "resolved" {
		return fmt.Sprintf("告警已于 %s 恢复", alert.EndsAt.Format(time.RFC3339))
	}
	return "告警仍未恢复 — 需要人工介入"
}

func (s *Service) FormatReport(report *retrospective.PostmortemReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# 故障复盘报告: %s\n\n", report.IncidentTitle))

	// === 基本信息 ===
	sb.WriteString("## 📊 基本信息\n\n")
	sb.WriteString(fmt.Sprintf("- **报告 ID**: %s\n", report.ID))
	sb.WriteString(fmt.Sprintf("- **严重级别**: %s\n", report.Severity))
	sb.WriteString(fmt.Sprintf("- **持续时间**: %s\n", report.Duration))
	sb.WriteString(fmt.Sprintf("- **开始时间**: %s\n", report.StartTime.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **结束时间**: %s\n", report.EndTime.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **生成时间**: %s\n", report.GeneratedAt.Format(time.RFC3339)))
	if report.DetectionMethod != "" {
		sb.WriteString(fmt.Sprintf("- **检测方式**: %s\n", report.DetectionMethod))
	}
	if report.MTTD != "" {
		sb.WriteString(fmt.Sprintf("- **MTTD（平均检测时间）**: %s\n", report.MTTD))
	}
	sb.WriteString("\n")

	// === 业务上下文 ===
	if report.BusinessContext != nil {
		bc := report.BusinessContext
		sb.WriteString("## 🏢 业务上下文\n\n")
		sb.WriteString(fmt.Sprintf("- **业务应用**: %s\n", bc.AppName))
		sb.WriteString(fmt.Sprintf("- **所属团队**: %s\n", bc.Team))
		sb.WriteString(fmt.Sprintf("- **关键度**: %s\n", bc.Criticality))
		if bc.BusinessUnit != "" {
			sb.WriteString(fmt.Sprintf("- **业务线**: %s\n", bc.BusinessUnit))
		}
		if bc.Environment != "" {
			sb.WriteString(fmt.Sprintf("- **环境**: %s\n", bc.Environment))
		}
		sb.WriteString("\n")
	}

	// === 受影响服务 ===
	if len(report.ImpactedSvc) > 0 {
		sb.WriteString("## 🏢 受影响服务\n\n")
		for _, svc := range report.ImpactedSvc {
			sb.WriteString(fmt.Sprintf("- %s\n", svc))
		}
		sb.WriteString("\n")
	}

	// === 工作负载上下文 ===
	if report.WorkloadContext != nil {
		wc := report.WorkloadContext
		sb.WriteString("## 📦 工作负载上下文\n\n")
		sb.WriteString(fmt.Sprintf("- **控制器**: %s/%s\n", wc.ControllerKind, wc.ControllerName))
		sb.WriteString(fmt.Sprintf("- **健康副本**: %d/%d\n", wc.HealthyPods, wc.TotalPods))
		if len(wc.Pods) > 0 {
			sb.WriteString("\n| Pod 名称 | 状态 |\n|----------|------|\n")
			for _, p := range wc.Pods {
				status := "🟢 健康"
				if p.IsAlerted {
					status = "🔴 告警"
				}
				sb.WriteString(fmt.Sprintf("| %s | %s |\n", p.Name, status))
			}
		}
		sb.WriteString("\n")
	}

	// === 事件时间线 ===
	if len(report.Timeline) > 0 {
		sb.WriteString("## 📅 事件时间线\n\n")
		sb.WriteString("| 时间 | 类型 | 描述 | 资源 |\n|------|------|------|------|\n")
		for _, e := range report.Timeline {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				e.Timestamp.Format("15:04:05"), e.EventType, e.Description, e.Resource))
		}
		sb.WriteString("\n")
	}

	// === AI 诊断结论 ===
	if report.AIDiagnosis != nil {
		ad := report.AIDiagnosis
		sb.WriteString("## 🤖 AI 诊断结论\n\n")
		sb.WriteString(fmt.Sprintf("**摘要**: %s\n\n", ad.Summary))
		sb.WriteString(fmt.Sprintf("**置信度**: %.0f%%\n\n", ad.Confidence*100))
		if len(ad.Evidence) > 0 {
			sb.WriteString("**证据链**:\n\n")
			for _, ev := range ad.Evidence {
				sb.WriteString(fmt.Sprintf("- %s\n", ev))
			}
			sb.WriteString("\n")
		}
		if ad.Remediation != "" {
			sb.WriteString(fmt.Sprintf("**建议**: %s\n\n", ad.Remediation))
		}
	}

	// === 根因 ===
	if report.RootCause.Final != "" {
		sb.WriteString("## 📝 根因\n\n")
		sb.WriteString(fmt.Sprintf("%s\n\n", report.RootCause.Final))
	}

	// === 因果链 ===
	if len(report.CausalChain.Links) > 0 {
		sb.WriteString("## 🔗 因果链\n\n")
		for _, link := range report.CausalChain.Links {
			sb.WriteString(fmt.Sprintf("- %s → %s (置信度: %.0f%%)\n", link.Cause, link.Effect, link.Confidence*100))
			if len(link.Evidence) > 0 {
				sb.WriteString(fmt.Sprintf("  - 证据: %s\n", strings.Join(link.Evidence, "; ")))
			}
		}
		sb.WriteString("\n")
	}

	// === 业务调用链 ===
	if report.BusinessCalls != nil && (len(report.BusinessCalls.Upstreams) > 0 || len(report.BusinessCalls.Downstreams) > 0) {
		bc := report.BusinessCalls
		sb.WriteString("## 🔗 业务调用链\n\n")
		sb.WriteString(fmt.Sprintf("**中心服务**: %s\n\n", bc.AppName))
		if len(bc.Upstreams) > 0 {
			sb.WriteString("**上游调用方**:\n\n")
			sb.WriteString("| 应用名 | 团队 | 关键度 |\n|--------|------|--------|\n")
			for _, u := range bc.Upstreams {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", u.AppName, u.Team, u.Criticality))
			}
			sb.WriteString("\n")
		}
		if len(bc.Downstreams) > 0 {
			sb.WriteString("**下游依赖方**:\n\n")
			sb.WriteString("| 应用名 | 团队 | 关键度 |\n|--------|------|--------|\n")
			for _, d := range bc.Downstreams {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", d.AppName, d.Team, d.Criticality))
			}
			sb.WriteString("\n")
		}
	}

	// === 业务影响 ===
	if report.BusinessImpact != nil {
		bi := report.BusinessImpact
		sb.WriteString("## 📊 业务影响\n\n")
		sb.WriteString(fmt.Sprintf("- **风险等级**: %s\n", bi.RiskLevel))
		if len(bi.DirectImpacts) > 0 {
			sb.WriteString("\n**直接影响**:\n\n")
			for _, d := range bi.DirectImpacts {
				sb.WriteString(fmt.Sprintf("- %s (团队: %s, 关键度: %s)\n", d.AppName, d.Team, d.Criticality))
				if d.Reasoning != "" {
					sb.WriteString(fmt.Sprintf("  - 原因: %s\n", d.Reasoning))
				}
			}
			sb.WriteString("\n")
		}
		if len(bi.IndirectImpacts) > 0 {
			sb.WriteString("**间接影响**:\n\n")
			for _, d := range bi.IndirectImpacts {
				sb.WriteString(fmt.Sprintf("- %s (团队: %s, 关键度: %s)\n", d.AppName, d.Team, d.Criticality))
				if d.ImpactPath != "" {
					sb.WriteString(fmt.Sprintf("  - 影响路径: %s\n", d.ImpactPath))
				}
			}
			sb.WriteString("\n")
		}
	}

	// === 影响评估详情 ===
	if report.ImpactAssessment != nil {
		ia := report.ImpactAssessment
		sb.WriteString("## 📊 影响评估\n\n")
		sb.WriteString(fmt.Sprintf("- **严重级别**: %s\n", ia.Severity))
		sb.WriteString(fmt.Sprintf("- **影响半径**: %d 跳\n", ia.BlastRadius))
		if len(ia.DirectImpact) > 0 {
			sb.WriteString("\n**🔴 直接影响资源**:\n\n")
			sb.WriteString("| 类型 | 名称 | 命名空间 |\n|------|------|----------|\n")
			for _, d := range ia.DirectImpact {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", d.Kind, d.Name, d.Namespace))
			}
			sb.WriteString("\n")
		}
		if len(ia.IndirectImpact) > 0 {
			sb.WriteString("**🟡 间接影响资源**:\n\n")
			sb.WriteString("| 类型 | 名称 | 命名空间 |\n|------|------|----------|\n")
			for _, d := range ia.IndirectImpact {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", d.Kind, d.Name, d.Namespace))
			}
			sb.WriteString("\n")
		}
		if ia.UserFacingImpact {
			sb.WriteString("⚠️ **用户面受到影响**\n\n")
		}
	}

	// === 诊断指标快照 ===
	if len(report.DiagnosisMetrics) > 0 {
		sb.WriteString("## 📈 诊断指标快照\n\n")
		sb.WriteString("| 指标 | 资源 | 值 | 状态 |\n|------|------|-----|------|\n")
		for _, m := range report.DiagnosisMetrics {
			val := fmt.Sprintf("%.1f", m.Value)
			sb.WriteString(fmt.Sprintf("| %s | %s/%s | %s | %s |\n", m.MetricName, m.ResourceKind, m.ResourceName, val, m.Status))
		}
		sb.WriteString("\n")
	}

	// === 关键日志 ===
	if len(report.DiagnosisLogs) > 0 {
		sb.WriteString("## 📋 关键日志\n\n")
		errorCount := 0
		for _, l := range report.DiagnosisLogs {
			if strings.EqualFold(l.Level, "error") || strings.EqualFold(l.Level, "err") {
				if errorCount < 10 {
					sb.WriteString(fmt.Sprintf("- **[%s]** `%s`: %s\n", l.Level, l.Pod, l.Message))
					errorCount++
				}
			}
		}
		sb.WriteString("\n")
	}

	// === 解决措施 ===
	if report.Resolution.Final != "" {
		sb.WriteString("## ✅ 解决措施\n\n")
		sb.WriteString(fmt.Sprintf("%s\n\n", report.Resolution.Final))
	}

	// === 自愈执行记录 ===
	if len(report.ExecutionActions) > 0 {
		sb.WriteString("## 🔧 自愈执行记录\n\n")
		sb.WriteString("| 时间 | 动作 | 目标 | 风险 | 方式 | 结果 |\n|------|------|------|------|------|------|\n")
		for _, e := range report.ExecutionActions {
			mode := "待审批"
			if e.Success {
				if e.ApprovedBy != "" {
					mode = "人工审批(" + e.ApprovedBy + ")"
				} else if e.AutoExecuted {
					mode = "自动执行"
				}
			} else if e.ApprovedBy != "" {
				mode = "审批后拒绝"
			}
			status := "❌ 失败"
			if e.Success {
				status = "✅ 成功"
			} else if strings.Contains(e.Message, "pending") || strings.Contains(e.Message, "approval") {
				status = "⏳ 待审批"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
				e.Timestamp.Format("01-02 15:04:05"), e.Action, e.Target, e.Risk, mode, status))
		}
		sb.WriteString("\n")
	}

	// === 经验教训 ===
	if len(report.LessonsLearned) > 0 {
		sb.WriteString("## 💡 经验教训\n\n")
		for i, l := range report.LessonsLearned {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, l))
		}
		sb.WriteString("\n")
	}

	// === 做得好的 ===
	if len(report.WhatWentWell) > 0 {
		sb.WriteString("## ✅ 做得好的\n\n")
		for _, item := range report.WhatWentWell {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	// === 可以改进的 ===
	if len(report.WhatWentWrong) > 0 {
		sb.WriteString("## ❌ 可以改进的\n\n")
		for _, item := range report.WhatWentWrong {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	// === 促成因素 ===
	if len(report.ContributingFactors) > 0 {
		sb.WriteString("## 🔍 促成因素\n\n")
		for _, item := range report.ContributingFactors {
			sb.WriteString(fmt.Sprintf("- %s\n", item))
		}
		sb.WriteString("\n")
	}

	// === 改进项 ===
	if len(report.ActionItems) > 0 {
		sb.WriteString("## 🎯 改进项\n\n")
		sb.WriteString("| 改进项 | 优先级 | 负责人 | 截止日期 | 退出标准 |\n|--------|--------|--------|----------|----------|\n")
		for _, a := range report.ActionItems {
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				a.Description, a.Priority, a.Owner, a.DueDate, a.ExitCriteria))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (s *Service) savePostmortem(report *retrospective.PostmortemReport, fingerprint string, alert *alert_models.ProcessedAlert) {
	reportJSON, err := json.Marshal(report)
	if err != nil {
		s.logger.Warn("Failed to marshal postmortem", zap.Error(err))
		return
	}

	model := retrospective.PostmortemModel{
		Fingerprint: fingerprint,
		ReportJSON:  string(reportJSON),
	}

	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "fingerprint"}},
		DoUpdates: clause.AssignmentColumns([]string{"report_json", "updated_at"}),
	}).Create(&model).Error; err != nil {
		s.logger.Error("Failed to save postmortem", zap.Error(err))
	}

	go s.extractKnowledge(report, fingerprint)
	go s.saveFaultReportVector(report, alert)
	go s.saveResourceLinks(fingerprint, alert)
}

// UpdatePostmortemRequest 人工修正请求
type UpdatePostmortemRequest struct {
	RootCause      *string                    `json:"root_cause"`
	Resolution     *string                    `json:"resolution"`
	LessonsLearned []string                   `json:"lessons_learned"`
	ActionItems    []retrospective.ActionItem `json:"action_items"`
	UpdatedBy      string                     `json:"updated_by"`
}

// UpdatePostmortem 更新复盘报告的 LLM 生成字段（人工修正）
func (s *Service) UpdatePostmortem(ctx context.Context, fingerprint string, req UpdatePostmortemRequest) (*retrospective.PostmortemReport, error) {
	var model retrospective.PostmortemModel
	if err := s.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&model).Error; err != nil {
		return nil, fmt.Errorf("postmortem not found: %w", err)
	}

	var report retrospective.PostmortemReport
	if err := json.Unmarshal([]byte(model.ReportJSON), &report); err != nil {
		return nil, fmt.Errorf("failed to parse postmortem: %w", err)
	}

	// 迁移旧格式：如果 root_cause 是 string，转为 EditableField
	migrateToEditableField(&report)

	if req.RootCause != nil {
		report.RootCause.Final = *req.RootCause
	}
	if req.Resolution != nil {
		report.Resolution.Final = *req.Resolution
	}
	if req.LessonsLearned != nil {
		report.LessonsLearned = req.LessonsLearned
	}
	if req.ActionItems != nil {
		report.ActionItems = req.ActionItems
	}

	reportJSON, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal postmortem: %w", err)
	}

	model.ReportJSON = string(reportJSON)
	model.UpdatedBy = req.UpdatedBy
	if err := s.db.WithContext(ctx).Save(&model).Error; err != nil {
		return nil, fmt.Errorf("failed to update postmortem: %w", err)
	}

	return &report, nil
}

// migrateToEditableField 兼容旧格式：将 string 类型转为 EditableField
func migrateToEditableField(report *retrospective.PostmortemReport) {
	var raw map[string]json.RawMessage
	data, _ := json.Marshal(report)
	_ = json.Unmarshal(data, &raw)

	if v, ok := raw["root_cause"]; ok {
		var ef retrospective.EditableField
		if err := json.Unmarshal(v, &ef); err != nil {
			var oldStr string
			if err2 := json.Unmarshal(v, &oldStr); err2 == nil {
				report.RootCause = retrospective.EditableField{
					AIGenerated: oldStr,
					Final:       oldStr,
				}
			}
		}
	}
	if v, ok := raw["resolution"]; ok {
		var ef retrospective.EditableField
		if err := json.Unmarshal(v, &ef); err != nil {
			var oldStr string
			if err2 := json.Unmarshal(v, &oldStr); err2 == nil {
				report.Resolution = retrospective.EditableField{
					AIGenerated: oldStr,
					Final:       oldStr,
				}
			}
		}
	}
}

func (s *Service) extractKnowledge(report *retrospective.PostmortemReport, fingerprint string) {
	faultType := inferFaultType(report.RootCause.Final, report.IncidentTitle)
	resourceKind := ""
	if len(report.Timeline) > 0 {
		resourceKind = report.Timeline[0].Resource
	}
	keywords := extractKeywords(report.RootCause.Final)
	summary := report.Resolution.Final
	if len(summary) > 500 {
		summary = summary[:500]
	}

	knowledge := retrospective.IncidentKnowledge{
		Fingerprint:       fingerprint,
		FaultType:         faultType,
		ResourceKind:      resourceKind,
		RootCauseKeywords: strings.Join(keywords, ","),
		ResolutionSummary: summary,
	}

	if err := s.db.Create(&knowledge).Error; err != nil {
		s.logger.Error("Failed to save incident knowledge", zap.Error(err))
	}
}

func (s *Service) WithHybridRetriever(hr *diagnosis_svc.HybridRetriever) *Service {
	s.hybridRetriever = hr
	return s
}

// WithExecutor 注入自愈执行器，复盘报告将包含该告警的审批/执行审计记录
func (s *Service) WithExecutor(exec interfaces.Executor) *Service {
	s.executor = exec
	return s
}

func (s *Service) SearchKnowledge(ctx context.Context, query string, faultType string, resourceKind string, resourceUID string, namespace string, limit int) ([]retrospective.IncidentKnowledge, error) {
	if limit <= 0 {
		limit = 20
	}

	if s.hybridRetriever != nil && resourceUID != "" {
		req := diagnosis.HybridSearchRequest{
			ResourceUID:    resourceUID,
			ResourceKind:   resourceKind,
			Namespace:      namespace,
			QueryText:      query,
			Limit:          limit,
			SemanticWeight: 0.5,
		}
		hybridResults, err := s.hybridRetriever.Search(ctx, req)
		if err != nil {
			s.logger.Warn("HybridRetriever failed in SearchKnowledge, fallback to SQL",
				zap.Error(err))
		} else if len(hybridResults) > 0 {
			fingerprints := make([]string, len(hybridResults))
			for i, r := range hybridResults {
				fingerprints[i] = r.Fingerprint
			}
			var knowledge []retrospective.IncidentKnowledge
			s.db.WithContext(ctx).
				Where("fingerprint IN ?", fingerprints).
				Order("created_at DESC").
				Find(&knowledge)
			return knowledge, nil
		}
	}

	var results []retrospective.IncidentKnowledge
	tx := s.db.Model(&retrospective.IncidentKnowledge{})
	if query != "" {
		like := "%" + query + "%"
		tx = tx.Where("root_cause_keywords LIKE ? OR resolution_summary LIKE ?", like, like)
	}
	if faultType != "" {
		tx = tx.Where("fault_type = ?", faultType)
	}
	if resourceKind != "" {
		tx = tx.Where("resource_kind = ?", resourceKind)
	}
	return results, tx.Order("created_at DESC").Limit(limit).Find(&results).Error
}

func (s *Service) GetPostmortemByFingerprint(ctx context.Context, fingerprint string) (*retrospective.PostmortemModel, error) {
	var model retrospective.PostmortemModel
	err := s.db.Where("fingerprint = ?", fingerprint).First(&model).Error
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *Service) GetPostmortemsByResourceUID(ctx context.Context, resourceUID string, limit int) ([]retrospective.PostmortemModel, error) {
	if limit <= 0 {
		limit = 10
	}

	var links []retrospective.PostmortemResourceLink
	if err := s.db.WithContext(ctx).
		Where("resource_uid = ?", resourceUID).
		Order("created_at DESC").
		Limit(limit).
		Find(&links).Error; err != nil {
		return nil, fmt.Errorf("query resource links: %w", err)
	}
	if len(links) == 0 {
		return nil, nil
	}

	fingerprints := make([]string, len(links))
	for i, l := range links {
		fingerprints[i] = l.Fingerprint
	}

	var reports []retrospective.PostmortemModel
	if err := s.db.WithContext(ctx).
		Where("fingerprint IN ?", fingerprints).
		Order("created_at DESC").
		Find(&reports).Error; err != nil {
		return nil, fmt.Errorf("query postmortems: %w", err)
	}
	return reports, nil
}

func inferFaultType(rootCause, title string) string {
	lower := strings.ToLower(rootCause + " " + title)
	switch {
	case strings.Contains(lower, "oom") || strings.Contains(lower, "memory"):
		return "OOM"
	case strings.Contains(lower, "crashloop") || strings.Contains(lower, "crash"):
		return "CrashLoop"
	case strings.Contains(lower, "imagepull") || strings.Contains(lower, "image"):
		return "ImagePull"
	case strings.Contains(lower, "network") || strings.Contains(lower, "timeout"):
		return "NetworkTimeout"
	case strings.Contains(lower, "disk") || strings.Contains(lower, "storage"):
		return "DiskPressure"
	default:
		return "Unknown"
	}
}

func extractKeywords(rootCause string) []string {
	kwList := []string{
		"oom", "crash", "restart", "timeout", "disk", "memory",
		"cpu", "network", "imagepull", "eviction", "notready",
	}
	var found []string
	lower := strings.ToLower(rootCause)
	for _, kw := range kwList {
		if strings.Contains(lower, kw) {
			found = append(found, kw)
		}
	}
	return found
}

func (s *Service) enrichBusinessCallsFromNebula(report *retrospective.PostmortemReport, appName, namespace string) {
	if appName == "" || s.graphDB == nil {
		return
	}

	result := &retrospective.BusinessReportCalls{AppName: appName}

	upstreamQ := fmt.Sprintf(
		`MATCH (caller:BusinessApp)-[:CallsApp]->(target:BusinessApp{app_name:%s,namespace:%s})
		 RETURN caller.app_name AS name, caller.team AS team, caller.criticality AS crit LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	if rs, err := s.graphDB.ExecuteAndCheck(upstreamQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, _ := row.GetValueByColName("name")
				team, _ := row.GetValueByColName("team")
				crit, _ := row.GetValueByColName("crit")
				n, _ := name.AsString()
				t, _ := team.AsString()
				c, _ := crit.AsString()
				if n != "" {
					result.Upstreams = append(result.Upstreams, retrospective.BusinessAppRef{
						AppName: n, Team: t, Criticality: c,
					})
				}
			}
		}
	}

	downstreamQ := fmt.Sprintf(
		`MATCH (target:BusinessApp{app_name:%s,namespace:%s})-[:CallsApp]->(callee:BusinessApp)
		 RETURN callee.app_name AS name, callee.team AS team, callee.criticality AS crit LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	if rs, err := s.graphDB.ExecuteAndCheck(downstreamQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, _ := row.GetValueByColName("name")
				team, _ := row.GetValueByColName("team")
				crit, _ := row.GetValueByColName("crit")
				n, _ := name.AsString()
				t, _ := team.AsString()
				c, _ := crit.AsString()
				if n != "" {
					result.Downstreams = append(result.Downstreams, retrospective.BusinessAppRef{
						AppName: n, Team: t, Criticality: c,
					})
				}
			}
		}
	}

	if len(result.Upstreams) > 0 || len(result.Downstreams) > 0 {
		report.BusinessCalls = result
	}
}

func (s *Service) enrichWorkloadContext(report *retrospective.PostmortemReport, podName, namespace string) {
	if s.graphDB == nil || podName == "" {
		return
	}

	ownerQ := fmt.Sprintf(
		`MATCH (pod:K8sResource{kind:"Pod",name:%s,name_space:%s})-[:OwnedBy*1..2]->(owner:K8sResource)
		 WHERE owner.K8sResource.kind IN ["Deployment","StatefulSet","DaemonSet","CronJob"]
		 RETURN owner.K8sResource.name AS name, owner.K8sResource.kind AS kind, owner.K8sResource.name_space AS ns
		 LIMIT 1`,
		strconv.Quote(podName), strconv.Quote(namespace),
	)

	rs, err := s.graphDB.ExecuteAndCheck(ownerQ)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return
	}

	row, _ := rs.GetRowValuesByIndex(0)
	on, _ := row.GetValueByColName("name")
	ok, _ := row.GetValueByColName("kind")
	ons, _ := row.GetValueByColName("ns")
	cname, _ := on.AsString()
	ckind, _ := ok.AsString()
	cns, _ := ons.AsString()

	siblingQ := fmt.Sprintf(
		`MATCH (sibling:K8sResource{kind:"Pod",name_space:%s})-[:OwnedBy*1..2]->(owner:K8sResource{name:%s,kind:%s})
		 RETURN sibling.K8sResource.name AS name, sibling.K8sResource.is_deleted AS deleted
		 ORDER BY sibling.K8sResource.name`,
		strconv.Quote(cns), strconv.Quote(cname), strconv.Quote(ckind),
	)

	srs, err := s.graphDB.ExecuteAndCheck(siblingQ)
	if err != nil || srs == nil {
		return
	}

	wc := &retrospective.WorkloadReportContext{
		ControllerKind: ckind,
		ControllerName: cname,
	}

	for i := 0; i < srs.GetRowSize(); i++ {
		r, err := srs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		nv, _ := r.GetValueByColName("name")
		dv, _ := r.GetValueByColName("deleted")
		sName, _ := nv.AsString()
		isDel, _ := dv.AsBool()
		wc.Pods = append(wc.Pods, retrospective.WorkloadPodItem{
			Name:      sName,
			IsAlerted: sName == podName,
		})
		wc.TotalPods++
		if !isDel {
			wc.HealthyPods++
		}
	}

	report.WorkloadContext = wc
}

func (s *Service) enrichRootCauseFallback(report *retrospective.PostmortemReport) {
	if s.llmProvider == nil {
		s.logger.Info("Root cause fallback skipped: no LLM provider configured")
		return
	}

	s.logger.Info("Starting LLM root cause fallback",
		zap.String("incident", report.IncidentTitle),
		zap.String("severity", report.Severity))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := interfaces.DiagnosisPrompt{
		Alert: map[string]string{
			"incident_title":  report.IncidentTitle,
			"severity":        report.Severity,
			"duration":        report.Duration,
			"root_cause_hint": report.RootCause.Final,
			"impacted_svcs":   strings.Join(report.ImpactedSvc, ", "),
		},
	}

	result, err := s.llmProvider.Diagnose(ctx, prompt)
	if err != nil {
		s.logger.Warn("LLM root cause analysis failed",
			zap.String("incident", report.IncidentTitle), zap.Error(err))
		return
	}

	if result.RootCause != "" {
		report.RootCause.Final = result.RootCause
		report.AIDiagnosis = &retrospective.AIDiagnosisSummary{
			RootCause:  result.RootCause,
			Confidence: float64(result.Confidence),
			Evidence:   result.Evidence,
			Summary:    result.RootCause,
		}
		s.logger.Info("Root cause fallback succeeded",
			zap.String("root_cause", result.RootCause[:min(100, len(result.RootCause))]))
	} else {
		s.logger.Warn("LLM root cause analysis returned empty result")
		return
	}
	if len(result.Remediation) > 0 {
		rem := result.Remediation[0]
		report.AIDiagnosis.Remediation = rem.Action
		report.Resolution.Final = rem.Description
	}
}

func (s *Service) enrichFromDiagnosisCache(report *retrospective.PostmortemReport, fingerprint string) {
	if s.diagCache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := s.diagCache.GetCachedDiagnosisResult(ctx, fingerprint)
	if err != nil || result == nil {
		s.logger.Info("No cached diagnosis, falling back to LLM root cause analysis",
			zap.String("fingerprint", fingerprint), zap.Error(err))
		s.enrichRootCauseFallback(report)
		return
	}

	s.logger.Info("Using cached diagnosis result for postmortem",
		zap.String("fingerprint", fingerprint),
		zap.Int("root_causes", len(result.RootCauses)))

	if len(result.RootCauses) > 0 {
		rc := result.RootCauses[0]
		rcDescription := fmt.Sprintf("%s/%s (置信度: %.0f%%)", rc.ResourceType, rc.ResourceName, rc.Confidence*100)
		if result.Summary != "" {
			rcDescription = result.Summary
		}
		report.RootCause.Final = rcDescription
		report.AIDiagnosis = &retrospective.AIDiagnosisSummary{
			RootCause:  rcDescription,
			Confidence: rc.Confidence,
			Evidence:   rc.Evidence,
			Summary:    result.Summary,
		}
		if len(result.Remediations) > 0 {
			report.AIDiagnosis.Remediation = result.Remediations[0].Action
			report.Resolution.Final = result.Remediations[0].Description
		}
	}

	if result.BusinessAppCalls != nil {
		var upstreams, downstreams []retrospective.BusinessAppRef
		for _, u := range result.BusinessAppCalls.Upstreams {
			upstreams = append(upstreams, retrospective.BusinessAppRef{
				AppName: u.AppName, Team: u.Team, Criticality: u.Criticality,
			})
		}
		for _, d := range result.BusinessAppCalls.Downstreams {
			downstreams = append(downstreams, retrospective.BusinessAppRef{
				AppName: d.AppName, Team: d.Team, Criticality: d.Criticality,
			})
		}
		report.BusinessCalls = &retrospective.BusinessReportCalls{
			AppName:     result.BusinessAppCalls.AppName,
			Upstreams:   upstreams,
			Downstreams: downstreams,
		}
	}

	if result.Impact.BusinessImpact != nil {
		bi := result.Impact.BusinessImpact
		var direct, indirect []retrospective.BusinessImpactItem
		for _, d := range bi.DirectImpacts {
			direct = append(direct, retrospective.BusinessImpactItem{
				AppName: d.AppName, Criticality: d.Criticality,
				Team: d.Team, ImpactPath: d.ImpactPath, Reasoning: d.Reasoning,
			})
		}
		for _, i := range bi.IndirectImpacts {
			indirect = append(indirect, retrospective.BusinessImpactItem{
				AppName: i.AppName, Criticality: i.Criticality,
				Team: i.Team, ImpactPath: i.ImpactPath, Reasoning: i.Reasoning,
			})
		}
		if len(direct) > 0 || len(indirect) > 0 {
			report.BusinessImpact = &retrospective.BusinessReportImpact{
				DirectImpacts:   direct,
				IndirectImpacts: indirect,
				RiskLevel:       bi.RiskLevel,
			}
		}
	}

	// 提取全量诊断数据（丰富化）
	report.AllRootCauses = result.RootCauses
	report.AllRemediations = result.Remediations

	if result.TopologySnapshot != nil {
		report.TopologySnapshot = result.TopologySnapshot
	}
	report.DiagnosisMetrics = result.Metrics
	report.DiagnosisLogs = result.RecentLogs
	report.ImpactAssessment = &result.Impact
	report.RelatedAlerts = result.RelatedAlerts
}

// enrichFromExecutionRecords 把该告警的自愈执行审计（提议/审批/执行）写入复盘报告
func (s *Service) enrichFromExecutionRecords(ctx context.Context, report *retrospective.PostmortemReport, fingerprint string) {
	if s.executor == nil || fingerprint == "" {
		return
	}
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	logs, err := s.executor.GetAuditLogs(qctx, map[string]string{"fingerprint": fingerprint})
	if err != nil {
		s.logger.Warn("Failed to load execution audit for postmortem", zap.String("fingerprint", fingerprint), zap.Error(err))
		return
	}
	if len(logs) == 0 {
		return
	}

	var successActions []string
	for _, l := range logs {
		rec := retrospective.ExecutionActionRecord{
			Timestamp:    l.Timestamp,
			Action:       string(l.Plan.Action),
			Target:       l.Plan.Target,
			Risk:         string(l.Plan.Risk),
			ApprovedBy:   l.ApprovedBy,
			AutoExecuted: l.AutoExecuted,
			Success:      l.Result.Success,
			Message:      l.Result.Message,
			Reason:       l.Plan.Reason,
		}
		report.ExecutionActions = append(report.ExecutionActions, rec)

		// 时间线事件：提议/审批/执行各归其位
		eventType := "remediation_proposed"
		desc := fmt.Sprintf("自愈提议: %s → %s（待审批）", l.Plan.Action, l.Plan.Target)
		if l.Result.Success {
			eventType = "remediation_executed"
			if l.ApprovedBy != "" {
				desc = fmt.Sprintf("人工审批并执行成功: %s → %s（审批人: %s）", l.Plan.Action, l.Plan.Target, l.ApprovedBy)
			} else if l.AutoExecuted {
				desc = fmt.Sprintf("自动模式执行成功: %s → %s", l.Plan.Action, l.Plan.Target)
			} else {
				desc = fmt.Sprintf("执行成功: %s → %s", l.Plan.Action, l.Plan.Target)
			}
			successActions = append(successActions, fmt.Sprintf("%s → %s", l.Plan.Action, l.Plan.Target))
		} else if l.ApprovedBy != "" {
			eventType = "remediation_rejected"
			desc = fmt.Sprintf("审批后执行被护栏拒绝: %s → %s（%s）", l.Plan.Action, l.Plan.Target, l.Result.Message)
		}
		report.Timeline = append(report.Timeline, retrospective.TimelineEvent{
			Timestamp:   l.Timestamp,
			EventType:   eventType,
			Description: desc,
			Source:      "executor",
			Severity:    "info",
			Resource:    l.Plan.Target,
		})
	}

	// 按时间排序，保持时间线有序
	sort.Slice(report.Timeline, func(i, j int) bool {
		return report.Timeline[i].Timestamp.Before(report.Timeline[j].Timestamp)
	})

	// 有成功执行的修复动作时，补充到解决措施
	if len(successActions) > 0 {
		report.Resolution.Final = fmt.Sprintf("%s\n\n已通过自愈执行完成修复: %s", report.Resolution.Final, strings.Join(successActions, "; "))
	}
}

func (s *Service) enrichWithLLM(report *retrospective.PostmortemReport, alert *alert_models.ProcessedAlert, fingerprint string) {
	timelineText := s.FormatReport(report)

	// 构建因果链文本摘要
	causalChainText := ""
	if len(report.CausalChain.Links) > 0 {
		var links []string
		for _, l := range report.CausalChain.Links {
			links = append(links, fmt.Sprintf("%s → %s (置信度: %.0f%%)", l.Cause, l.Effect, l.Confidence*100))
		}
		causalChainText = strings.Join(links, "; ")
	}

	// 构建影响摘要
	impactSummary := ""
	if report.ImpactAssessment != nil {
		impactSummary = fmt.Sprintf("影响半径: %d, 直接受影响: %d, 间接影响: %d, 用户面影响: %v",
			report.ImpactAssessment.BlastRadius,
			len(report.ImpactAssessment.DirectImpact),
			len(report.ImpactAssessment.IndirectImpact),
			report.ImpactAssessment.UserFacingImpact)
	}

	// 构建诊断摘要
	diagnosisSummary := ""
	if report.AIDiagnosis != nil {
		diagnosisSummary = report.AIDiagnosis.Summary
	}

	// 构建指标摘要
	metricsSummary := ""
	if len(report.DiagnosisMetrics) > 0 {
		var ms []string
		for _, m := range report.DiagnosisMetrics {
			ms = append(ms, fmt.Sprintf("%s/%s %s=%.1f (%s)", m.ResourceKind, m.ResourceName, m.MetricName, m.Value, m.Status))
		}
		metricsSummary = strings.Join(ms, "; ")
	}

	// 构建日志高亮（取前 5 条 Error 日志）
	logsHighlights := ""
	var errCount int
	for _, l := range report.DiagnosisLogs {
		if strings.EqualFold(l.Level, "error") || strings.EqualFold(l.Level, "err") {
			if errCount < 5 {
				logsHighlights += fmt.Sprintf("[%s] %s: %s\n", l.Timestamp, l.Pod, l.Message)
				errCount++
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	insights, err := s.llmProvider.GeneratePostmortemInsights(ctx, interfaces.PostmortemInsightsPrompt{
		IncidentTitle:    report.IncidentTitle,
		Severity:         report.Severity,
		Duration:         report.Duration,
		RootCause:        report.RootCause.Final,
		Timeline:         timelineText,
		AlertName:        alert.Labels["alertname"],
		ResourceKind:     alert.ResourceType,
		ResourceName:     alert.ResourceName,
		Namespace:        alert.Namespace,
		CausalChain:      causalChainText,
		ImpactSummary:    impactSummary,
		DiagnosisSummary: diagnosisSummary,
		AffectedServices: report.ImpactedSvc,
		MetricsSummary:   metricsSummary,
		LogsHighlights:   logsHighlights,
	})
	if err != nil {
		s.logger.Warn("LLM postmortem insights failed, using rules",
			zap.String("fingerprint", fingerprint), zap.Error(err))
		return
	}

	if len(insights.LessonsLearned) > 0 {
		report.LessonsLearned = insights.LessonsLearned
	}
	if len(insights.WhatWentWell) > 0 {
		report.WhatWentWell = insights.WhatWentWell
	}
	if len(insights.WhatWentWrong) > 0 {
		report.WhatWentWrong = insights.WhatWentWrong
	}
	if len(insights.ContributingFactors) > 0 {
		report.ContributingFactors = insights.ContributingFactors
	}
	if len(insights.ActionItems) > 0 {
		var items []retrospective.ActionItem
		for _, a := range insights.ActionItems {
			items = append(items, retrospective.ActionItem{
				Description:  a.Description,
				Owner:        a.Owner,
				Priority:     a.Priority,
				DueDate:      time.Now().Add(72 * time.Hour).Format("2006-01-02"),
				Status:       "open",
				ExitCriteria: a.ExitCriteria,
				Category:     a.Category,
			})
		}
		report.ActionItems = items
	}
	if insights.Resolution != "" {
		report.Resolution.Final = insights.Resolution
	}
}

func (s *Service) saveFaultReportVector(report *retrospective.PostmortemReport, alert *alert_models.ProcessedAlert) {
	if s.db == nil || alert == nil {
		return
	}

	summary := fmt.Sprintf(
		"[%s] %s/%s: %s. 根因: %s. 解决方案: %s",
		report.Severity,
		alert.ResourceType, alert.ResourceName,
		report.IncidentTitle,
		report.RootCause.Final,
		report.Resolution.Final,
	)

	record := diagnosis.FaultReportVector{
		ReportID:         report.ID,
		AlertFingerprint: alert.Fingerprint,
		Summary:          summary,
		ResourceKind:     alert.ResourceType,
		ResourceName:     alert.ResourceName,
		Namespace:        alert.Namespace,
		FaultType:        inferFaultType(report.RootCause.Final, report.IncidentTitle),
	}

	if s.llmProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if emb, err := s.llmProvider.GenerateEmbedding(ctx, summary); err == nil && len(emb) > 0 {
			record.Embedding = emb
		} else {
			s.logger.Debug("Embedding generation skipped",
				zap.String("fingerprint", alert.Fingerprint), zap.Error(err))
		}
	}

	if err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "report_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"summary", "resource_kind", "resource_name",
			"namespace", "fault_type", "alert_fingerprint",
		}),
	}).Create(&record).Error; err != nil {
		s.logger.Error("Failed to save fault report vector",
			zap.String("fingerprint", alert.Fingerprint), zap.Error(err))
	}
}

func (s *Service) saveResourceLinks(fingerprint string, alert *alert_models.ProcessedAlert) {
	if s.db == nil || alert == nil || alert.ResourceUID == "" {
		return
	}

	link := retrospective.PostmortemResourceLink{
		Fingerprint:  fingerprint,
		ResourceUID:  alert.ResourceUID,
		ResourceKind: alert.ResourceType,
		ResourceName: alert.ResourceName,
		Namespace:    alert.Namespace,
	}

	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
		s.logger.Warn("Failed to save resource link",
			zap.String("fingerprint", fingerprint), zap.Error(err))
	}
}

type PostmortemListItem struct {
	Fingerprint      string    `json:"fingerprint"`
	IncidentTitle    string    `json:"incident_title"`
	Severity         string    `json:"severity"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	Duration         string    `json:"duration"`
	ResourceKind     string    `json:"resource_kind"`
	ResourceName     string    `json:"resource_name"`
	Namespace        string    `json:"namespace"`
	RootCauseSummary string    `json:"root_cause_summary"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ListFilter struct {
	Severity     string
	ResourceKind string
	ResourceName string
	Namespace    string
	Keyword      string
	StartTime    string
	EndTime      string
	FaultType    string
}

type ListResult struct {
	Items    []PostmortemListItem `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func (s *Service) ListPostmortems(ctx context.Context, filter ListFilter, page, pageSize int) (*ListResult, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not configured")
	}

	query := s.db.Model(&retrospective.PostmortemModel{})

	if filter.Severity != "" {
		query = query.Where("report_json::jsonb->>'severity' = ?", filter.Severity)
	}
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		query = query.Where("report_json::jsonb->>'incident_title' ILIKE ?", kw)
	}
	if filter.StartTime != "" {
		if t, err := parseTimeFlexible(filter.StartTime); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if filter.EndTime != "" {
		if t, err := parseTimeFlexible(filter.EndTime); err == nil {
			query = query.Where("created_at <= ?", t)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count postmortems: %w", err)
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	var models []retrospective.PostmortemModel
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list postmortems: %w", err)
	}

	items := make([]PostmortemListItem, 0, len(models))
	for _, m := range models {
		var report retrospective.PostmortemReport
		if err := json.Unmarshal([]byte(m.ReportJSON), &report); err != nil {
			s.logger.Warn("Failed to unmarshal postmortem JSON for list",
				zap.String("fingerprint", m.Fingerprint), zap.Error(err))
			continue
		}

		resourceKind, resourceName, namespace := extractResourceInfo(&report, filter)

		if filter.ResourceKind != "" && resourceKind != filter.ResourceKind {
			continue
		}
		if filter.ResourceName != "" && !strings.Contains(strings.ToLower(resourceName), strings.ToLower(filter.ResourceName)) {
			continue
		}
		if filter.Namespace != "" && namespace != filter.Namespace {
			continue
		}

		rc := report.RootCause.Final
		if len(rc) > 100 {
			rc = rc[:100]
		}

		items = append(items, PostmortemListItem{
			Fingerprint:      m.Fingerprint,
			IncidentTitle:    report.IncidentTitle,
			Severity:         report.Severity,
			StartTime:        report.StartTime,
			EndTime:          report.EndTime,
			Duration:         report.Duration,
			ResourceKind:     resourceKind,
			ResourceName:     resourceName,
			Namespace:        namespace,
			RootCauseSummary: rc,
			CreatedAt:        m.CreatedAt,
			UpdatedAt:        m.UpdatedAt,
		})
	}

	return &ListResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func extractResourceInfo(report *retrospective.PostmortemReport, filter ListFilter) (kind, name, ns string) {
	if report.AIDiagnosis != nil {
		if report.AIDiagnosis.Summary != "" {
			return "", "", ""
		}
	}
	if report.WorkloadContext != nil {
		kind = report.WorkloadContext.ControllerKind
		name = report.WorkloadContext.ControllerName
	}
	if report.BusinessContext != nil && report.BusinessContext.AppName != "" {
		if name == "" {
			name = report.BusinessContext.AppName
		}
	}
	return
}

func parseTimeFlexible(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}
