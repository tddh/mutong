package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/allegro/bigcache/v3"
	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

// applyBusinessContext projects provider context into explicit BusinessContext + legacy EnrichTags.
func (e *AlertEnricher) applyBusinessContext(enriched *alert_models.EnrichedAlert, bizApp interfaces.BusinessAppContext, source string) {
	if bizApp.UID == "" {
		return
	}
	if source == "" {
		source = bizApp.Source
	}
	if source == "" {
		source = "unknown"
	}
	enriched.BusinessContext = alert_models.BusinessContext{
		UID:          bizApp.UID,
		AppName:      bizApp.AppName,
		Namespace:    bizApp.Namespace,
		BusinessUnit: bizApp.BusinessUnit,
		Team:         bizApp.Team,
		Criticality:  bizApp.Criticality,
		Environment:  bizApp.Environment,
		Source:       source,
	}
	enriched.EnrichTags["businessSource"] = source
	enriched.EnrichTags["businessApp"] = bizApp.AppName
	enriched.EnrichTags["businessUnit"] = bizApp.BusinessUnit
	enriched.EnrichTags["team"] = bizApp.Team
	enriched.EnrichTags["criticality"] = bizApp.Criticality
	if bizApp.Environment != "" {
		enriched.EnrichTags["environment"] = bizApp.Environment
	}
}

type topologyCacheEntry struct {
	ResourceUID string `json:"resourceUID"`
	NodeName    string `json:"nodeName"`
	OwnerKind   string `json:"ownerKind"`
	OwnerName   string `json:"ownerName"`
}

type AlertEnricher struct {
	logger         interfaces.Logger
	graphDB        interfaces.GraphDB
	topologyCache  *bigcache.BigCache
	bizCtxProvider interfaces.BusinessContextProvider
}

func NewAlertEnricher(logger interfaces.Logger, graphDB interfaces.GraphDB) alert_interfaces.AlertEnricher {
	cache, err := bigcache.New(context.Background(), bigcache.DefaultConfig(10*time.Minute))
	if err != nil {
		logger.Warn("Failed to create topology cache, running without cache", zap.Error(err))
		cache = nil
	}
	return &AlertEnricher{
		logger:        logger,
		graphDB:       graphDB,
		topologyCache: cache,
	}
}

func (e *AlertEnricher) WithBusinessContextProvider(p interfaces.BusinessContextProvider) {
	e.bizCtxProvider = p
}

func (e *AlertEnricher) Enrich(ctx context.Context, a *alert_models.Alert) (*alert_models.EnrichedAlert, error) {
	k8sLabels := alert_models.ExtractK8sLabels(a.Labels)

	enriched := &alert_models.EnrichedAlert{
		Alert:         *a,
		Namespace:     k8sLabels.Namespace,
		ResourceName:  e.getResourceName(k8sLabels),
		ResourceType:  e.getResourceType(k8sLabels),
		ResourceUID:   k8sLabels.ResourceUID,
		EnrichTags:    make(map[string]string),
		TopologyPath:  []string{},
		RelatedAlerts: []string{},
	}

	// Priority 1: UID exact query (only Pod provides UID from Prometheus)
	if k8sLabels.ResourceUID != "" {
		if err := e.enrichByUID(ctx, enriched, k8sLabels); err != nil {
			e.logger.Warn("UID enrichment failed, falling back to label-based",
				zap.String("uid", k8sLabels.ResourceUID),
				zap.Error(err))
			enriched.ResourceUID = ""
		} else {
			e.addEnrichTags(enriched, k8sLabels)
			return enriched, nil
		}
	}

	// Priority 2: Label-based query with is_deleted filter
	switch {
	case k8sLabels.Pod != "":
		if err := e.enrichFromPod(ctx, enriched, k8sLabels); err != nil {
			e.logger.Warn("Failed to enrich from pod", zap.Error(err))
		}
	case k8sLabels.Node != "":
		if err := e.enrichFromNode(ctx, enriched, k8sLabels); err != nil {
			e.logger.Warn("Failed to enrich from node", zap.Error(err))
		}
	case k8sLabels.Service != "":
		if err := e.enrichFromService(ctx, enriched, k8sLabels); err != nil {
			e.logger.Warn("Failed to enrich from service", zap.Error(err))
		}
	}

	e.addEnrichTags(enriched, k8sLabels)

	return enriched, nil
}

func (e *AlertEnricher) enrichByUID(ctx context.Context, enriched *alert_models.EnrichedAlert, k8sLabels alert_models.AlertLabels) error {
	query := fmt.Sprintf(`
		MATCH (v:K8sResource{uid: %s})
		OPTIONAL MATCH (v)-[:RunsOn]->(node:K8sResource{kind:'Node'})
		OPTIONAL MATCH (v)-[:OwnedBy]->(owner:K8sResource)
		RETURN 
			v.K8sResource.kind as kind,
			v.K8sResource.name as name,
			v.K8sResource.name_space as namespace,
			node.K8sResource.name as node_name,
			owner.K8sResource.kind as owner_kind,
			owner.K8sResource.name as owner_name
	`, strconv.Quote(k8sLabels.ResourceUID))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return fmt.Errorf("UID query failed: %w", err)
	}

	if resultSet.GetRowSize() == 0 {
		return fmt.Errorf("resource not found by uid: %s", k8sLabels.ResourceUID)
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return fmt.Errorf("failed to get row values: %w", err)
	}

	if kind, err := row.GetValueByColName("kind"); err == nil {
		if kindStr, _ := kind.AsString(); kindStr != "" {
			enriched.ResourceType = kindStr
		}
	}
	if name, err := row.GetValueByColName("name"); err == nil {
		if nameStr, _ := name.AsString(); nameStr != "" {
			enriched.ResourceName = nameStr
		}
	}
	if ns, err := row.GetValueByColName("namespace"); err == nil {
		if nsStr, _ := ns.AsString(); nsStr != "" {
			enriched.Namespace = nsStr
		}
	}
	if nodeName, err := row.GetValueByColName("node_name"); err == nil {
		if name, _ := nodeName.AsString(); name != "" {
			enriched.NodeName = name
		}
	}
	if ownerKind, err := row.GetValueByColName("owner_kind"); err == nil {
		if kind, _ := ownerKind.AsString(); kind != "" {
			enriched.OwnerKind = kind
		}
	}
	if ownerName, err := row.GetValueByColName("owner_name"); err == nil {
		if name, _ := ownerName.AsString(); name != "" {
			enriched.OwnerName = name
		}
	}

	enriched.TopologyPath = e.buildTopologyPath(enriched)

	if e.bizCtxProvider != nil && k8sLabels.ResourceUID != "" {
		bizApp := e.bizCtxProvider.EnrichAlertByResourceUID(k8sLabels.ResourceUID)
		e.applyBusinessContext(enriched, bizApp, "")
		e.enrichBizCalls(enriched)
		e.enrichStakeholders(enriched)
		enriched.BusinessImpact = e.buildBusinessImpactFromBusinessContext(enriched.BusinessContext)
	}

	return nil
}

func (e *AlertEnricher) enrichFromPod(ctx context.Context, enriched *alert_models.EnrichedAlert, k8sLabels alert_models.AlertLabels) error {
	cacheKey := k8sLabels.Namespace + "/Pod/" + k8sLabels.Pod
	if e.topologyCache != nil {
		if cached, err := e.topologyCache.Get(cacheKey); err == nil {
			var entry topologyCacheEntry
			if err := json.Unmarshal(cached, &entry); err == nil && entry.ResourceUID != "" {
				enriched.ResourceUID = entry.ResourceUID
				enriched.NodeName = entry.NodeName
				enriched.OwnerKind = entry.OwnerKind
				enriched.OwnerName = entry.OwnerName
				enriched.TopologyPath = e.buildTopologyPath(enriched)
			}
		}
	}

	if enriched.ResourceUID == "" {
		query := fmt.Sprintf(`
			MATCH (p:K8sResource{kind:'Pod',name:%s,name_space:%s,is_deleted:false})
			OPTIONAL MATCH (p)-[:RunsOn]->(n:K8sResource{kind:'Node'})
			OPTIONAL MATCH (p)-[:OwnedBy]->(d:K8sResource)
			RETURN p.K8sResource.uid as uid, n.K8sResource.name as nodeName, d.K8sResource.kind as ownerKind, d.K8sResource.name as ownerName
			ORDER BY uid DESC
			LIMIT 1
		`, strconv.Quote(k8sLabels.Pod), strconv.Quote(k8sLabels.Namespace))

		resultSet, err := e.graphDB.ExecuteAndCheck(query)
		if err != nil {
			return fmt.Errorf("failed to query pod topology: %w", err)
		}

		if resultSet.GetRowSize() == 0 {
			e.logger.Debug("Pod not found in topology",
				zap.String("pod", k8sLabels.Pod),
				zap.String("namespace", k8sLabels.Namespace))
			return nil
		}

		row, err := resultSet.GetRowValuesByIndex(0)
		if err != nil {
			return fmt.Errorf("failed to get row values: %w", err)
		}

		if uidWrapper, err := row.GetValueByColName("uid"); err == nil {
			if uidStr, err := uidWrapper.AsString(); err == nil {
				enriched.ResourceUID = uidStr
			}
		}

		if nodeNameWrapper, err := row.GetValueByColName("nodeName"); err == nil {
			if nodeName, err := nodeNameWrapper.AsString(); err == nil {
				enriched.NodeName = nodeName
			}
		}

		if ownerKindWrapper, err := row.GetValueByColName("ownerKind"); err == nil {
			if ownerKind, err := ownerKindWrapper.AsString(); err == nil {
				enriched.OwnerKind = ownerKind
			}
		}
		if ownerNameWrapper, err := row.GetValueByColName("ownerName"); err == nil {
			if ownerName, err := ownerNameWrapper.AsString(); err == nil {
				enriched.OwnerName = ownerName
			}
		}
	}

	enriched.TopologyPath = e.buildTopologyPath(enriched)

	if e.topologyCache != nil {
		entry := topologyCacheEntry{
			ResourceUID: enriched.ResourceUID,
			NodeName:    enriched.NodeName,
			OwnerKind:   enriched.OwnerKind,
			OwnerName:   enriched.OwnerName,
		}
		if data, err := json.Marshal(entry); err == nil {
			_ = e.topologyCache.Set(cacheKey, data)
		}
	}

	if e.bizCtxProvider != nil && enriched.ResourceUID != "" {
		bizApp := e.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
		e.applyBusinessContext(enriched, bizApp, "")
		e.enrichBizCalls(enriched)
		e.enrichStakeholders(enriched)
		enriched.BusinessImpact = e.buildBusinessImpactFromBusinessContext(enriched.BusinessContext)
	}

	return nil
}

func (e *AlertEnricher) enrichFromNode(ctx context.Context, enriched *alert_models.EnrichedAlert, k8sLabels alert_models.AlertLabels) error {
	query := fmt.Sprintf(`
		MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})
		OPTIONAL MATCH (p:K8sResource{kind:'Pod'})-[:RunsOn]->(n)
		OPTIONAL MATCH (p)-[:BelongsToApp]->(b:BusinessApp)
		RETURN n.K8sResource.uid as uid, count(p) as podCount,
			b.app_name as appName, b.criticality as criticality, b.team as team
		LIMIT 1
	`, strconv.Quote(k8sLabels.Node))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return fmt.Errorf("failed to query node topology: %w", err)
	}

	if resultSet.GetRowSize() == 0 {
		e.logger.Debug("Node not found in topology", zap.String("node", k8sLabels.Node))
		return nil
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return fmt.Errorf("failed to get row values: %w", err)
	}

	if uidWrapper, err := row.GetValueByColName("uid"); err == nil {
		if uidStr, err := uidWrapper.AsString(); err == nil {
			enriched.ResourceUID = uidStr
		}
	}

	if podCountWrapper, err := row.GetValueByColName("podCount"); err == nil {
		if podCount, err := podCountWrapper.AsInt(); err == nil {
			enriched.EnrichTags["node_pod_count"] = fmt.Sprintf("%d", podCount)
		}
	}

	enriched.TopologyPath = []string{"Node:" + k8sLabels.Node}

	enriched.BusinessImpact = e.buildBusinessImpactFromNode(ctx, k8sLabels.Node)

	// Node alerts belong to infrastructure ownership context.
	// Business impact inference is handled separately (B1 layer).
	enriched.BusinessContext = alert_models.BusinessContext{
		UID:    enriched.ResourceUID,
		Source: "node-infra",
	}
	enriched.EnrichTags["businessSource"] = "node-infra"

	return nil
}

func (e *AlertEnricher) buildBusinessImpactFromNode(ctx context.Context, nodeName string) alert_models.BusinessImpact {
	impact := alert_models.BusinessImpact{}

	query := fmt.Sprintf(`
		MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})
		MATCH (p:K8sResource{kind:'Pod'})-[:RunsOn]->(n)
		MATCH (p)-[:BelongsToApp]->(b:BusinessApp)
		RETURN distinct b.app_name as appName, b.criticality as criticality, b.team as team
	`, strconv.Quote(nodeName))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil {
		e.logger.Warn("Failed to query node business impact", zap.Error(err))
		return impact
	}

	seen := make(map[string]bool)
	var apps []alert_models.BusinessAppBrief
	hasCritical := false

	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}

		appName, criticality, team := "", "", ""
		if v, err := row.GetValueByColName("appName"); err == nil {
			appName, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("criticality"); err == nil {
			criticality, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("team"); err == nil {
			team, _ = v.AsString()
		}

		if appName == "" || seen[appName] {
			continue
		}
		seen[appName] = true

		apps = append(apps, alert_models.BusinessAppBrief{
			AppName:     appName,
			Criticality: criticality,
			Team:        team,
		})

		if criticality == "critical" || criticality == "P0" {
			hasCritical = true
		}
	}

	impact.AffectedBusinessApps = apps
	impact.AffectedAppCount = len(apps)
	impact.ContainsCriticalApps = hasCritical

	if len(apps) == 1 {
		impact.PrimaryBusinessApp = apps[0].AppName
	}

	return impact
}

func (e *AlertEnricher) buildBusinessImpactFromBusinessContext(bc alert_models.BusinessContext) alert_models.BusinessImpact {
	if bc.AppName == "" {
		return alert_models.BusinessImpact{}
	}

	app := alert_models.BusinessAppBrief{
		AppName:     bc.AppName,
		Criticality: bc.Criticality,
		Team:        bc.Team,
	}

	return alert_models.BusinessImpact{
		AffectedBusinessApps: []alert_models.BusinessAppBrief{app},
		AffectedAppCount:     1,
		PrimaryBusinessApp:   bc.AppName,
		ContainsCriticalApps: bc.Criticality == "critical" || bc.Criticality == "P0",
	}
}

func (e *AlertEnricher) enrichFromService(ctx context.Context, enriched *alert_models.EnrichedAlert, k8sLabels alert_models.AlertLabels) error {
	query := fmt.Sprintf(`
		MATCH (s:K8sResource{kind:'Service',name:%s,name_space:%s,is_deleted:false})
		OPTIONAL MATCH (s)-[:SvcToPods]->(p:K8sResource{kind:'Pod'})
		RETURN s.K8sResource.uid as uid, count(p) as podCount
		ORDER BY uid DESC
		LIMIT 1
	`, strconv.Quote(k8sLabels.Service), strconv.Quote(k8sLabels.Namespace))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return fmt.Errorf("failed to query service topology: %w", err)
	}

	if resultSet.GetRowSize() == 0 {
		e.logger.Debug("Service not found in topology",
			zap.String("service", k8sLabels.Service),
			zap.String("namespace", k8sLabels.Namespace))
		return nil
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return fmt.Errorf("failed to get row values: %w", err)
	}

	if uidWrapper, err := row.GetValueByColName("uid"); err == nil {
		if uidStr, err := uidWrapper.AsString(); err == nil {
			enriched.ResourceUID = uidStr
		}
	}

	if podCountWrapper, err := row.GetValueByColName("podCount"); err == nil {
		if podCount, err := podCountWrapper.AsInt(); err == nil {
			enriched.EnrichTags["service_pod_count"] = fmt.Sprintf("%d", podCount)
		}
	}

	enriched.TopologyPath = []string{"Service:" + k8sLabels.Service}

	if e.bizCtxProvider != nil && enriched.ResourceUID != "" {
		bizApp := e.bizCtxProvider.EnrichAlertByResourceUID(enriched.ResourceUID)
		e.applyBusinessContext(enriched, bizApp, "")
		e.enrichBizCalls(enriched)
		e.enrichStakeholders(enriched)
	}

	return nil
}

func (e *AlertEnricher) getResourceName(k8sLabels alert_models.AlertLabels) string {
	if k8sLabels.ResourceName != "" {
		return k8sLabels.ResourceName
	}
	if k8sLabels.ResourceKind != "" {
		switch k8sLabels.ResourceKind {
		case "Pod":
			return k8sLabels.Pod
		case "Node":
			return k8sLabels.Node
		case "Service":
			return k8sLabels.Service
		}
	}
	if k8sLabels.Pod != "" {
		return k8sLabels.Pod
	}
	if k8sLabels.Node != "" {
		return k8sLabels.Node
	}
	if k8sLabels.Service != "" {
		return k8sLabels.Service
	}
	return ""
}

func (e *AlertEnricher) getResourceType(k8sLabels alert_models.AlertLabels) string {
	if k8sLabels.ResourceType != "" {
		return k8sLabels.ResourceType
	}
	if k8sLabels.ResourceKind != "" {
		return k8sLabels.ResourceKind
	}
	if k8sLabels.Pod != "" {
		return "Pod"
	}
	if k8sLabels.Node != "" {
		return "Node"
	}
	if k8sLabels.Service != "" {
		return "Service"
	}
	return "Unknown"
}

func (e *AlertEnricher) buildTopologyPath(enriched *alert_models.EnrichedAlert) []string {
	var path []string

	if enriched.NodeName != "" {
		path = append(path, "Node:"+enriched.NodeName)
	}

	if enriched.ResourceType == "Pod" && enriched.ResourceName != "" {
		path = append(path, "Pod:"+enriched.ResourceName)
	}

	if enriched.OwnerKind != "" && enriched.OwnerName != "" {
		path = append(path, fmt.Sprintf("%s:%s", enriched.OwnerKind, enriched.OwnerName))
	}

	return path
}

func (e *AlertEnricher) addEnrichTags(enriched *alert_models.EnrichedAlert, k8sLabels alert_models.AlertLabels) {
	if k8sLabels.AlertName != "" {
		enriched.EnrichTags["alertname"] = k8sLabels.AlertName
	}
	if k8sLabels.Severity != "" {
		enriched.EnrichTags["severity"] = k8sLabels.Severity
	}
	if k8sLabels.Container != "" {
		enriched.EnrichTags["container"] = k8sLabels.Container
	}

	enriched.EnrichTags["topology_depth"] = fmt.Sprintf("%d", len(enriched.TopologyPath))
}

func (e *AlertEnricher) FindRelatedAlerts(ctx context.Context, resourceType, resourceName string) ([]string, error) {
	if resourceType == "Node" {
		query := fmt.Sprintf(`
			MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})
			MATCH (p:K8sResource{kind:'Pod'})-[:RunsOn]->(n)
			RETURN p.name as podName
		`, strconv.Quote(resourceName))

		resultSet, err := e.graphDB.ExecuteAndCheck(query)
		if err != nil {
			return nil, fmt.Errorf("failed to find related pods: %w", err)
		}

		var relatedPods []string
		for i := 0; i < resultSet.GetRowSize(); i++ {
			row, err := resultSet.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			if podNameWrapper, err := row.GetValueByColName("podName"); err == nil {
				if podName, err := podNameWrapper.AsString(); err == nil {
					relatedPods = append(relatedPods, podName)
				}
			}
		}

		return relatedPods, nil
	}

	return nil, nil
}

func ParseTopologyPath(path []string) []struct {
	Type string
	Name string
} {
	var result []struct {
		Type string
		Name string
	}

	for _, p := range path {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) == 2 {
			result = append(result, struct {
				Type string
				Name string
			}{
				Type: parts[0],
				Name: parts[1],
			})
		}
	}

	return result
}

func (e *AlertEnricher) enrichBizCalls(enriched *alert_models.EnrichedAlert) {
	if enriched.BusinessContext.AppName == "" {
		return
	}
	appName := enriched.BusinessContext.AppName
	ns := enriched.BusinessContext.Namespace

	e.logger.Debug("enrichBizCalls called", zap.String("appName", appName), zap.String("ns", ns))

	calls := &diagnosis.BusinessAppCalls{AppName: appName, Namespace: ns}

	// 上游：谁调用了这个 BusinessApp
	upQ := fmt.Sprintf(
		`MATCH (caller:BusinessApp)-[:CallsApp]->(target:BusinessApp{app_name:%s,namespace:%s})
		 RETURN caller.BusinessApp.app_name AS name, caller.BusinessApp.team AS team, caller.BusinessApp.criticality AS crit LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(ns),
	)
	if rs, err := e.graphDB.ExecuteAndCheck(upQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name := colStr(row, "name")
				team := colStr(row, "team")
				crit := colStr(row, "crit")
				if name != "" {
					calls.Upstreams = append(calls.Upstreams, diagnosis.BusinessAppRef{AppName: name, Team: team, Criticality: crit})
				}
			}
		}
	}

	// 下游：这个 BusinessApp 调用了谁
	downQ := fmt.Sprintf(
		`MATCH (target:BusinessApp{app_name:%s,namespace:%s})-[:CallsApp]->(callee:BusinessApp)
		 RETURN callee.BusinessApp.app_name AS name, callee.BusinessApp.team AS team, callee.BusinessApp.criticality AS crit LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(ns),
	)
	if rs, err := e.graphDB.ExecuteAndCheck(downQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name := colStr(row, "name")
				team := colStr(row, "team")
				crit := colStr(row, "crit")
				if name != "" {
					calls.Downstreams = append(calls.Downstreams, diagnosis.BusinessAppRef{AppName: name, Team: team, Criticality: crit})
				}
			}
		}
	}

	if len(calls.Upstreams) > 0 || len(calls.Downstreams) > 0 {
		enriched.BusinessCalls = calls
	}
	e.logger.Debug("enrichBizCalls done",
		zap.Int("upstreams", len(calls.Upstreams)),
		zap.Int("downstreams", len(calls.Downstreams)))
}

func colStr(row *nebula.Record, col string) string {
	v, e := row.GetValueByColName(col)
	if e != nil {
		return ""
	}
	s, _ := v.AsString()
	return s
}

func determineStakeholderStrategy(alert *alert_models.ProcessedAlert) string {
	if alert.BusinessContext.AppName == "" {
		return "none"
	}
	if alert.ResourceType == "Node" {
		return "node_pods"
	}
	st := alert.BusinessContext.ServiceType
	if st == "middleware" {
		an := strings.ToLower(alert.Labels["alertname"])
		if strings.Contains(an, "lag") || strings.Contains(an, "consumer") {
			return "upstream_consumer"
		}
		if strings.Contains(an, "broker") || strings.Contains(an, "oom") {
			return "upstream_all"
		}
		return "none"
	}
	return "upstream_callers"
}

func (e *AlertEnricher) enrichStakeholders(enriched *alert_models.EnrichedAlert) {
	strategy := determineStakeholderStrategy(
		&alert_models.ProcessedAlert{EnrichedAlert: *enriched},
	)
	e.logger.Debug("enrichStakeholders: strategy determined",
		zap.String("strategy", strategy),
		zap.String("appName", enriched.BusinessContext.AppName),
		zap.String("serviceType", enriched.BusinessContext.ServiceType),
		zap.String("resourceType", enriched.ResourceType))
	if strategy == "none" {
		e.logger.Debug("enrichStakeholders: no stakeholders for this alert")
		return
	}

	var teams []string
	switch strategy {
	case "node_pods":
		teams = e.queryTeamsOnNode(enriched.NodeName)
	case "upstream_all", "upstream_consumer":
		if enriched.BusinessContext.AppName != "" {
			teams = e.queryUpstreamTeams(enriched.BusinessContext.AppName, enriched.BusinessContext.Namespace)
		}
	case "upstream_callers":
		if enriched.BusinessContext.AppName != "" {
			teams = e.queryUpstreamTeams(enriched.BusinessContext.AppName, enriched.BusinessContext.Namespace)
		}
	}

	var stakeholders []alert_models.StakeholderInfo
	seen := map[string]bool{enriched.BusinessContext.Team: true}
	for _, team := range teams {
		if seen[team] {
			continue
		}
		seen[team] = true
		stakeholders = append(stakeholders, alert_models.StakeholderInfo{
			Team: team, Channel: "slack", ImpactLevel: "indirect",
		})
	}
	enriched.Stakeholders = stakeholders
	e.logger.Debug("enrichStakeholders: done",
		zap.Int("stakeholderCount", len(stakeholders)),
		zap.Int("totalCandidates", len(teams)))
}

func (e *AlertEnricher) queryTeamsOnNode(nodeName string) []string {
	if nodeName == "" || e.graphDB == nil {
		return nil
	}
	query := fmt.Sprintf(
		`MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})<-[:RunsOn]-(p:K8sResource{kind:'Pod',is_deleted:false})
		 MATCH (p)-[:BelongsToApp]->(b:BusinessApp)
		 RETURN DISTINCT b.BusinessApp.team AS team LIMIT 20`,
		strconv.Quote(nodeName),
	)
	rs, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || rs == nil {
		return nil
	}
	var teams []string
	seen := make(map[string]bool)
	for i := 0; i < rs.GetRowSize(); i++ {
		row, _ := rs.GetRowValuesByIndex(i)
		team := colStr(row, "team")
		if team != "" && !seen[team] {
			seen[team] = true
			teams = append(teams, team)
		}
	}
	return teams
}

func (e *AlertEnricher) queryUpstreamTeams(appName, namespace string) []string {
	if appName == "" || e.graphDB == nil {
		return nil
	}
	query := fmt.Sprintf(
		`MATCH (caller:BusinessApp)-[:CallsApp]->(:BusinessApp{app_name:%s,namespace:%s})
		 RETURN DISTINCT caller.BusinessApp.team AS team LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)
	rs, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || rs == nil {
		return nil
	}
	var teams []string
	seen := make(map[string]bool)
	for i := 0; i < rs.GetRowSize(); i++ {
		row, _ := rs.GetRowValuesByIndex(i)
		team := colStr(row, "team")
		if team != "" && !seen[team] {
			seen[team] = true
			teams = append(teams, team)
		}
	}
	return teams
}

// ---- External Alert Enrichment ----

// EnrichFromExternal processes external alert enrichment using the configured bridge strategy.
func (e *AlertEnricher) EnrichFromExternal(ctx context.Context, enriched *alert_models.EnrichedAlert, source *alert_models.AlertSource) {
	e.logger.Info("Enriching external alert",
		zap.String("source", source.Name),
		zap.String("strategy", string(source.EnrichmentStrategy)),
		zap.String("fingerprint", enriched.Fingerprint))

	switch source.EnrichmentStrategy {
	case alert_models.StrategyNodeAffinity:
		e.enrichByNodeAffinity(ctx, enriched, source)
	case alert_models.StrategyServiceGraph:
		e.enrichByServiceGraph(ctx, enriched, source)
	case alert_models.StrategyDirectBusinessApp:
		e.enrichByDirectBusinessApp(ctx, enriched, source)
	case alert_models.StrategyBusinessLabels:
		e.enrichFromBusinessLabels(ctx, enriched, source)
	case alert_models.StrategyNone:
		e.logger.Info("External alert enrichment disabled (strategy=none)",
			zap.String("source", source.Name))
	default:
		e.logger.Warn("Unknown enrichment strategy, falling back to node_affinity",
			zap.String("strategy", string(source.EnrichmentStrategy)))
		e.enrichByNodeAffinity(ctx, enriched, source)
	}
}

// enrichByNodeAffinity bridges via physical node: host → K8s Node → Pod → BusinessApp.
func (e *AlertEnricher) enrichByNodeAffinity(ctx context.Context, enriched *alert_models.EnrichedAlert, source *alert_models.AlertSource) {
	nodeName := enriched.Labels["node"]
	if nodeName == "" {
		e.logger.Warn("node_affinity strategy requires 'node' label",
			zap.String("source", source.Name))
		return
	}

	e.logger.Info("Enriching external alert via node_affinity",
		zap.String("source", source.Name),
		zap.String("node", nodeName))

	enriched.BusinessImpact = e.buildBusinessImpactFromNode(ctx, nodeName)

	query := fmt.Sprintf(`
		MATCH (n:K8sResource{kind:'Node',name:%s,is_deleted:false})
		MATCH (p:K8sResource{kind:'Pod',is_deleted:false})-[:RunsOn]->(n)
		MATCH (p)-[:BelongsToApp]->(b:BusinessApp)
		RETURN DISTINCT b.BusinessApp.uid AS uid,
			b.BusinessApp.app_name AS appName,
			b.BusinessApp.team AS team,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.criticality AS criticality,
			b.BusinessApp.environment AS environment,
			b.BusinessApp.business_unit AS businessUnit
		LIMIT 10
	`, strconv.Quote(nodeName))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet.GetRowSize() == 0 {
		e.logger.Info("No BusinessApp found on node, using businessMapping defaults",
			zap.String("node", nodeName),
			zap.Error(err))
		fallbackBC := alert_models.BusinessContext{
			Team:         source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality:  source.BusinessMapping.Criticality,
			Source:       source.Name,
			ServiceType:  source.BusinessMapping.ServiceType,
		}
		enriched.BusinessContext = fallbackBC
		enriched.EnrichTags["businessSource"] = source.Name
		enriched.EnrichTags["team"] = source.BusinessMapping.Team
		enriched.ResourceType = "Node"
		enriched.ResourceName = nodeName
		enriched.NodeName = nodeName
		return
	}

	row, _ := resultSet.GetRowValuesByIndex(0)
	bc := alert_models.BusinessContext{
		UID:          colStr(row, "uid"),
		AppName:      colStr(row, "appName"),
		Namespace:    colStr(row, "namespace"),
		Team:         colStr(row, "team"),
		BusinessUnit: colStr(row, "businessUnit"),
		Criticality:  colStr(row, "criticality"),
		Environment:  colStr(row, "environment"),
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}

	enriched.BusinessContext = bc
	enriched.ResourceType = "Node"
	enriched.ResourceName = nodeName
	enriched.NodeName = nodeName
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	e.logger.Info("Node affinity enrichment completed",
		zap.String("node", nodeName),
		zap.String("appName", bc.AppName),
		zap.String("team", bc.Team))

	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}

// enrichByServiceGraph bridges via service topology: serviceName → BusinessApp → CallsApp.
func (e *AlertEnricher) enrichByServiceGraph(ctx context.Context, enriched *alert_models.EnrichedAlert, source *alert_models.AlertSource) {
	serviceName := enriched.Labels["serviceName"]
	if serviceName == "" {
		serviceName = enriched.Labels["appName"]
	}
	if serviceName == "" {
		e.logger.Warn("service_graph strategy requires 'serviceName' or 'appName' label",
			zap.String("source", source.Name))
		return
	}

	e.logger.Info("Enriching external alert via service_graph",
		zap.String("source", source.Name),
		zap.String("serviceName", serviceName))

	query := fmt.Sprintf(`
		MATCH (b:BusinessApp)
		WHERE b.BusinessApp.app_name == %s
		RETURN b.BusinessApp.uid AS uid,
			b.BusinessApp.app_name AS appName,
			b.BusinessApp.team AS team,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.criticality AS criticality,
			b.BusinessApp.environment AS environment,
			b.BusinessApp.business_unit AS businessUnit
		LIMIT 1
	`, strconv.Quote(serviceName))

	resultSet, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet.GetRowSize() == 0 {
		e.logger.Info("BusinessApp not found for service, using businessMapping",
			zap.String("serviceName", serviceName),
			zap.Error(err))
		enriched.BusinessContext = alert_models.BusinessContext{
			AppName:      serviceName,
			Team:         source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality:  source.BusinessMapping.Criticality,
			Source:       source.Name,
			ServiceType:  source.BusinessMapping.ServiceType,
		}
		enriched.EnrichTags["businessSource"] = source.Name
		enriched.EnrichTags["team"] = source.BusinessMapping.Team
		return
	}

	row, _ := resultSet.GetRowValuesByIndex(0)
	bc := alert_models.BusinessContext{
		UID:          colStr(row, "uid"),
		AppName:      colStr(row, "appName"),
		Namespace:    colStr(row, "namespace"),
		Team:         colStr(row, "team"),
		BusinessUnit: colStr(row, "businessUnit"),
		Criticality:  colStr(row, "criticality"),
		Environment:  colStr(row, "environment"),
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}
	enriched.BusinessContext = bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	e.logger.Info("Service graph enrichment completed",
		zap.String("serviceName", serviceName),
		zap.String("appName", bc.AppName),
		zap.String("team", bc.Team))

	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}

// enrichByDirectBusinessApp matches appName directly to BusinessApp.
func (e *AlertEnricher) enrichByDirectBusinessApp(ctx context.Context, enriched *alert_models.EnrichedAlert, source *alert_models.AlertSource) {
	appName := enriched.Labels["appName"]
	if appName == "" {
		appName = source.BusinessMapping.AppName
	}
	if appName == "" {
		e.logger.Warn("direct_business_app strategy requires 'appName' label",
			zap.String("source", source.Name))
		return
	}

	e.logger.Info("Enriching external alert via direct_business_app",
		zap.String("source", source.Name),
		zap.String("appName", appName))

	bc := alert_models.BusinessContext{
		AppName:      appName,
		Team:         enriched.Labels["team"],
		BusinessUnit: enriched.Labels["businessUnit"],
		Criticality:  enriched.Labels["criticality"],
		Source:       source.Name,
		ServiceType:  source.BusinessMapping.ServiceType,
	}
	if bc.Team == "" {
		bc.Team = source.BusinessMapping.Team
	}
	if bc.Criticality == "" {
		bc.Criticality = source.BusinessMapping.Criticality
	}
	if bc.BusinessUnit == "" {
		bc.BusinessUnit = source.BusinessMapping.BusinessUnit
	}

	query := fmt.Sprintf(`
		MATCH (b:BusinessApp{app_name:%s})
		RETURN b.BusinessApp.uid AS uid,
			b.BusinessApp.namespace AS namespace,
			b.BusinessApp.environment AS environment
		LIMIT 1
	`, strconv.Quote(appName))
	if resultSet, err := e.graphDB.ExecuteAndCheck(query); err == nil && resultSet.GetRowSize() > 0 {
		row, _ := resultSet.GetRowValuesByIndex(0)
		bc.UID = colStr(row, "uid")
		bc.Namespace = colStr(row, "namespace")
		if bc.Environment == "" {
			bc.Environment = colStr(row, "environment")
		}
	}

	enriched.BusinessContext = bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	e.logger.Info("Direct business app enrichment completed",
		zap.String("appName", appName),
		zap.String("team", bc.Team))

	e.enrichBizCalls(enriched)
	e.enrichStakeholders(enriched)
}

// enrichFromBusinessLabels extracts business context directly from alert labels (path B).
func (e *AlertEnricher) enrichFromBusinessLabels(ctx context.Context, enriched *alert_models.EnrichedAlert, source *alert_models.AlertSource) {
	e.logger.Info("Enriching external alert via business_labels (path B)",
		zap.String("source", source.Name))

	bc := source.ExtractBusinessLabels(enriched.Labels)
	if bc == nil {
		e.logger.Info("No business labels found, using businessMapping defaults",
			zap.String("source", source.Name))
		enriched.BusinessContext = alert_models.BusinessContext{
			Team:         source.BusinessMapping.Team,
			BusinessUnit: source.BusinessMapping.BusinessUnit,
			Criticality:  source.BusinessMapping.Criticality,
			Source:       source.Name,
			ServiceType:  source.BusinessMapping.ServiceType,
		}
		enriched.EnrichTags["businessSource"] = source.Name
		return
	}

	enriched.BusinessContext = *bc
	enriched.EnrichTags["businessSource"] = source.Name
	enriched.EnrichTags["businessApp"] = bc.AppName
	enriched.EnrichTags["team"] = bc.Team
	enriched.EnrichTags["businessUnit"] = bc.BusinessUnit

	e.logger.Info("Business labels enrichment completed",
		zap.String("appName", bc.AppName),
		zap.String("team", bc.Team),
		zap.String("businessUnit", bc.BusinessUnit))

	if bc.AppName != "" && e.graphDB != nil {
		e.enrichBizCalls(enriched)
		e.enrichStakeholders(enriched)
	}
}
