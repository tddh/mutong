package diagnosis

import (
	"fmt"
	"strings"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type ImpactAssessor struct {
	logger  interfaces.Logger
	querier *TopologyQuerier
}

func NewImpactAssessor(logger interfaces.Logger, querier *TopologyQuerier) *ImpactAssessor {
	return &ImpactAssessor{
		logger:  logger,
		querier: querier,
	}
}

func (a *ImpactAssessor) AssessImpact(resourceUID, resourceKind, resourceName, namespace, severity string) diagnosis.ImpactAssessment {
	return a.AssessImpactWithBusiness(resourceUID, resourceKind, resourceName, namespace, severity, alert_models.BusinessContext{}, alert_models.BusinessImpact{})
}

func (a *ImpactAssessor) AssessImpactWithBusiness(resourceUID, resourceKind, resourceName, namespace, severity string, bizCtx alert_models.BusinessContext, bizImpact alert_models.BusinessImpact) diagnosis.ImpactAssessment {
	impact := diagnosis.ImpactAssessment{
		Severity: severity,
	}

	uid := resourceUID
	if uid == "" && resourceKind != "" && resourceName != "" {
		uid = a.querier.ResolveUID(resourceKind, resourceName, namespace)
	}

	if uid == "" {
		a.logger.Warn("Cannot resolve resource UID for impact assessment",
			zap.String("kind", resourceKind),
			zap.String("name", resourceName))
		return impact
	}

	allResources := a.querier.QueryTopologyResources(uid, 3)
	impact.BlastRadius = len(allResources)
	impact.AffectedResources = formatResources(allResources)

	impact.DirectImpact = a.querier.QueryTopologyResources(uid, 1)
	impact.IndirectImpact = excludeResources(allResources, impact.DirectImpact)
	impact.PotentialImpact = a.querier.QuerySameNodeOrNamespace(uid, resourceName, namespace)

	impact.AffectedServices = a.querier.FindAffectedServices(uid)
	impact.AffectedIngresses = a.querier.FindAffectedIngresses(uid)
	impact.UserFacingImpact = len(impact.AffectedIngresses) > 0

	// 注入业务上下文
	impact.BusinessContext = formatBusinessContext(bizCtx)
	impact.BusinessImpactNote = formatBusinessImpact(bizImpact)
	impact.AffectedAppNames = collectAffectedAppNames(bizImpact)

	a.logger.Debug("Impact assessment completed",
		zap.Int("blast_radius", impact.BlastRadius),
		zap.Int("direct_impact", len(impact.DirectImpact)),
		zap.Int("indirect_impact", len(impact.IndirectImpact)),
		zap.Bool("user_facing", impact.UserFacingImpact))

	return impact
}

func formatBusinessContext(ctx alert_models.BusinessContext) string {
	if ctx.AppName == "" && ctx.Team == "" && ctx.Criticality == "" {
		return ""
	}
	var parts []string
	if ctx.AppName != "" {
		parts = append(parts, "应用: "+ctx.AppName)
	}
	if ctx.Team != "" {
		parts = append(parts, "团队: "+ctx.Team)
	}
	if ctx.Criticality != "" {
		parts = append(parts, "关键度: "+ctx.Criticality)
	}
	if ctx.BusinessUnit != "" {
		parts = append(parts, "业务线: "+ctx.BusinessUnit)
	}
	if ctx.Environment != "" {
		parts = append(parts, "环境: "+ctx.Environment)
	}
	return strings.Join(parts, ", ")
}

func formatBusinessImpact(impact alert_models.BusinessImpact) string {
	if impact.AffectedAppCount == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("影响 %d 个业务应用", impact.AffectedAppCount)}
	if impact.ContainsCriticalApps {
		parts = append(parts, "含关键应用")
	}
	if impact.PrimaryBusinessApp != "" {
		parts = append(parts, "主要应用: "+impact.PrimaryBusinessApp)
	}
	return strings.Join(parts, ", ")
}

func collectAffectedAppNames(impact alert_models.BusinessImpact) []string {
	names := make([]string, 0, len(impact.AffectedBusinessApps))
	for _, app := range impact.AffectedBusinessApps {
		if app.AppName != "" {
			names = append(names, app.AppName)
		}
	}
	return names
}

func formatResources(resources []diagnosis.ResourceRef) []string {
	result := make([]string, len(resources))
	for i, r := range resources {
		result[i] = fmt.Sprintf("%s/%s", r.Kind, r.Name)
	}
	return result
}

func excludeResources(all, exclude []diagnosis.ResourceRef) []diagnosis.ResourceRef {
	excludeSet := make(map[string]bool, len(exclude))
	for _, r := range exclude {
		excludeSet[r.UID] = true
	}

	var result []diagnosis.ResourceRef
	for _, r := range all {
		if !excludeSet[r.UID] {
			result = append(result, r)
		}
	}
	return result
}
