package services

import (
	"encoding/json"
	"fmt"
	"strconv"

	"gitee.com/tddh/mutong/interfaces"
)

type BusinessTopologyService struct {
	logger  interfaces.Logger
	graphDB interfaces.GraphDB
}

func NewBusinessTopologyService(logger interfaces.Logger, graphDB interfaces.GraphDB) *BusinessTopologyService {
	return &BusinessTopologyService{
		logger:  logger,
		graphDB: graphDB,
	}
}

type BusinessAppNode struct {
	UID          string `json:"id"`
	AppName      string `json:"appName"`
	Namespace    string `json:"namespace"`
	Criticality  string `json:"criticality"`
	Environment  string `json:"environment"`
	Team         string `json:"team"`
	BusinessUnit string `json:"businessUnit"`
	OwnerName    string `json:"ownerName"`
	OwnerKind    string `json:"ownerKind"`
	Provenance   string `json:"provenance"` // "label"=资源建模 / "trace"=链路推断
}

type CallEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

type BusinessTopologyGraph struct {
	Nodes []BusinessAppNode `json:"nodes"`
	Edges []CallEdge        `json:"edges"`
}

func (s *BusinessTopologyService) GetApps(businessUnit, team, namespace string) ([]BusinessAppNode, error) {
	conditions := []string{}
	if businessUnit != "" {
		conditions = append(conditions, fmt.Sprintf("v.BusinessApp.business_unit = %s", strconv.Quote(businessUnit)))
	}
	if team != "" {
		conditions = append(conditions, fmt.Sprintf("v.BusinessApp.team = %s", strconv.Quote(team)))
	}
	if namespace != "" {
		conditions = append(conditions, fmt.Sprintf("v.BusinessApp.namespace = %s", strconv.Quote(namespace)))
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			whereClause += " AND " + c
		}
	}

	query := fmt.Sprintf(`MATCH (v:BusinessApp)%s RETURN v.BusinessApp.uid as uid, v.BusinessApp.app_name as app_name, v.BusinessApp.namespace as namespace, v.BusinessApp.criticality as criticality, v.BusinessApp.environment as environment, v.BusinessApp.team as team, v.BusinessApp.business_unit as business_unit, v.BusinessApp.owner_name as owner_name, v.BusinessApp.owner_kind as owner_kind`, whereClause)

	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, fmt.Errorf("query BusinessApp failed: %w", err)
	}

	var apps []BusinessAppNode
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}

		app := BusinessAppNode{}
		if v, err := row.GetValueByColName("uid"); err == nil {
			app.UID, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("app_name"); err == nil {
			app.AppName, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("namespace"); err == nil {
			app.Namespace, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("criticality"); err == nil {
			app.Criticality, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("environment"); err == nil {
			app.Environment, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("team"); err == nil {
			app.Team, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("business_unit"); err == nil {
			app.BusinessUnit, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("owner_name"); err == nil {
			app.OwnerName, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("owner_kind"); err == nil {
			app.OwnerKind, _ = v.AsString()
		}
		apps = append(apps, app)
	}

	// provenance：有 BelongsToApp 入边=资源建模(label)，否则=链路推断(trace)。
	// 一次性查出有成员的顶点集合，避免逐点查询。
	memberUIDs := s.appUIDsWithMembers()
	for i := range apps {
		if memberUIDs[apps[i].UID] {
			apps[i].Provenance = "label"
		} else {
			apps[i].Provenance = "trace"
		}
	}

	return apps, nil
}

// appUIDsWithMembers 返回所有拥有 BelongsToApp 入边（即有真实 K8s 资源指向）的业务顶点 uid 集合。
func (s *BusinessTopologyService) appUIDsWithMembers() map[string]bool {
	set := make(map[string]bool)
	query := `MATCH (v:BusinessApp)<-[:BelongsToApp]-() RETURN DISTINCT id(v) AS uid`
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		return set
	}
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, e := resultSet.GetRowValuesByIndex(i)
		if e != nil {
			continue
		}
		if uid, ue := getStrVal(row, "uid"); ue == nil && uid != "" {
			set[uid] = true
		}
	}
	return set
}

func (s *BusinessTopologyService) GetCalls() ([]CallEdge, error) {
	query := `MATCH (a:BusinessApp)-[e:CallsApp]->(b:BusinessApp) RETURN src(e) as source, dst(e) as target`

	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, fmt.Errorf("query CallsApp failed: %w", err)
	}

	var edges []CallEdge
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}

		edge := CallEdge{Relation: "calls"}
		if v, err := row.GetValueByColName("source"); err == nil {
			edge.Source, _ = v.AsString()
		}
		if v, err := row.GetValueByColName("target"); err == nil {
			edge.Target, _ = v.AsString()
		}
		edges = append(edges, edge)
	}

	return edges, nil
}

func (s *BusinessTopologyService) GetGraph(businessUnit, team string) (BusinessTopologyGraph, error) {
	apps, err := s.GetApps(businessUnit, team, "")
	if err != nil {
		return BusinessTopologyGraph{}, err
	}

	edges, err := s.GetCalls()
	if err != nil {
		return BusinessTopologyGraph{}, err
	}

	appUIDs := make(map[string]bool)
	for _, app := range apps {
		appUIDs[app.UID] = true
	}

	filteredEdges := make([]CallEdge, 0, len(edges))
	for _, edge := range edges {
		if appUIDs[edge.Source] && appUIDs[edge.Target] {
			filteredEdges = append(filteredEdges, edge)
		}
	}

	return BusinessTopologyGraph{
		Nodes: apps,
		Edges: filteredEdges,
	}, nil
}

// BusinessAppMember 业务顶点的成员资源（通过 BelongsToApp 入边反查得到）。
// 成员即「创建/构成该业务顶点的 K8s 资源」，是回答“哪个资源创建了这个顶点”的权威来源。
type BusinessAppMember struct {
	UID          string `json:"uid"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	IsDeleted    bool   `json:"isDeleted"`
	IsController bool   `json:"isController"`
}

// BusinessAppDetail 业务顶点详情，含创建来源（provenance）、归属工作负载与成员资源清单。
type BusinessAppDetail struct {
	UID          string              `json:"id"`
	AppName      string              `json:"appName"`
	Namespace    string              `json:"namespace"`
	Criticality  string              `json:"criticality"`
	Environment  string              `json:"environment"`
	Team         string              `json:"team"`
	BusinessUnit string              `json:"businessUnit"`
	OwnerName    string              `json:"ownerName"`
	OwnerKind    string              `json:"ownerKind"`
	Provenance   string              `json:"provenance"` // "label"=资源建模 / "trace"=链路推断
	PrimaryKind  string              `json:"primaryKind"`
	PrimaryName  string              `json:"primaryName"`
	PrimaryUID   string              `json:"primaryUid"`
	MemberCount  int                 `json:"memberCount"`
	Members      []BusinessAppMember `json:"members"`
}

// controllerRank 返回工作负载的“主体性”排序，数字越小越接近顶层控制器。
// 用于从成员资源里推导 primaryWorkload（如 Deployment 优于其 ReplicaSet/Pod）。
func controllerRank(kind string) int {
	switch kind {
	case "Deployment":
		return 1
	case "StatefulSet":
		return 2
	case "DaemonSet":
		return 3
	case "CronJob":
		return 4
	case "Job":
		return 5
	case "ReplicaSet":
		return 6
	default:
		return 99
	}
}

// GetAppDetail 返回单个业务顶点的完整来源信息。
// uid 即 BusinessApp 的 VID（bizapp-<cluster>-<ns>-<appName>）。
func (s *BusinessTopologyService) GetAppDetail(uid string) (BusinessAppDetail, error) {
	detail := BusinessAppDetail{UID: uid, Provenance: "trace"}
	if uid == "" {
		return detail, fmt.Errorf("empty business app uid")
	}

	// 1. 读顶点自身属性（owner_name/owner_kind 可能为 NULL，getStrVal 会安全降级为 ""）
	propQuery := fmt.Sprintf(`FETCH PROP ON BusinessApp %s YIELD BusinessApp.app_name AS app_name, BusinessApp.namespace AS namespace, BusinessApp.criticality AS criticality, BusinessApp.environment AS environment, BusinessApp.team AS team, BusinessApp.business_unit AS business_unit, BusinessApp.owner_name AS owner_name, BusinessApp.owner_kind AS owner_kind`,
		strconv.Quote(uid))
	if rs, err := s.graphDB.ExecuteAndCheck(propQuery); err == nil && rs != nil && rs.GetRowSize() > 0 {
		if row, e := rs.GetRowValuesByIndex(0); e == nil {
			detail.AppName, _ = getStrVal(row, "app_name")
			detail.Namespace, _ = getStrVal(row, "namespace")
			detail.Criticality, _ = getStrVal(row, "criticality")
			detail.Environment, _ = getStrVal(row, "environment")
			detail.Team, _ = getStrVal(row, "team")
			detail.BusinessUnit, _ = getStrVal(row, "business_unit")
			detail.OwnerName, _ = getStrVal(row, "owner_name")
			detail.OwnerKind, _ = getStrVal(row, "owner_kind")
		}
	}

	// 2. BelongsToApp 反查成员资源（权威“创建来源”）
	memberQuery := fmt.Sprintf(`MATCH (r:K8sResource)-[:BelongsToApp]->(v:BusinessApp) WHERE id(v) == %s RETURN id(r) AS uid, r.K8sResource.kind AS kind, r.K8sResource.name AS name, r.K8sResource.name_space AS ns, r.K8sResource.is_deleted AS del`,
		strconv.Quote(uid))
	rs, err := s.graphDB.ExecuteAndCheck(memberQuery)
	if err != nil {
		return detail, fmt.Errorf("query BelongsToApp members failed: %w", err)
	}
	if rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			row, e := rs.GetRowValuesByIndex(i)
			if e != nil {
				continue
			}
			m := BusinessAppMember{}
			m.UID, _ = getStrVal(row, "uid")
			m.Kind, _ = getStrVal(row, "kind")
			m.Name, _ = getStrVal(row, "name")
			m.Namespace, _ = getStrVal(row, "ns")
			if v, ve := row.GetValueByColName("del"); ve == nil {
				m.IsDeleted, _ = v.AsBool()
			}
			m.IsController = controllerRank(m.Kind) < 99
			detail.Members = append(detail.Members, m)
		}
	}
	detail.MemberCount = len(detail.Members)
	if detail.MemberCount > 0 {
		detail.Provenance = "label"
	}

	// 3. 从成员推导 primaryWorkload：优先未删除、controller 级、rank 最小者
	bestIdx := -1
	for i, m := range detail.Members {
		if bestIdx == -1 {
			bestIdx = i
			continue
		}
		b := detail.Members[bestIdx]
		// 未删除优先
		if b.IsDeleted && !m.IsDeleted {
			bestIdx = i
			continue
		}
		if m.IsDeleted && !b.IsDeleted {
			continue
		}
		// rank 小者优先
		if controllerRank(m.Kind) < controllerRank(b.Kind) {
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		detail.PrimaryKind = detail.Members[bestIdx].Kind
		detail.PrimaryName = detail.Members[bestIdx].Name
		detail.PrimaryUID = detail.Members[bestIdx].UID
	}

	return detail, nil
}

func (s *BusinessTopologyService) EnrichAlertByResourceUID(resourceUID string) interfaces.BusinessAppContext {
	// 1. 先从 Nebula 读该资源的 name + namespace + labels
	appName, namespace := s.getResourceAppLabels(resourceUID)
	if appName != "" {
		app := s.matchBusinessAppByName(appName, namespace)
		if app.UID != "" {
			return app
		}
	}

	// 2. 标签找不到则查直接 BelongsToApp
	app := s.queryBusinessApp(resourceUID, 0)
	if app.UID != "" {
		return app
	}

	// 3. 再找不到则沿 owner 链向上（Pod → ReplicaSet → Deployment → BusinessApp）
	for hops := 1; hops <= 2; hops++ {
		query := fmt.Sprintf(`
			MATCH (r:K8sResource{uid: %s,is_deleted:false})-[:OwnedBy*%d..%d]->(o:K8sResource{is_deleted:false})-[:BelongsToApp]->(a:BusinessApp)
			RETURN a.BusinessApp.uid as uid, a.BusinessApp.app_name as app_name, a.BusinessApp.namespace as namespace, a.BusinessApp.criticality as criticality, a.BusinessApp.environment as environment, a.BusinessApp.team as team, a.BusinessApp.business_unit as business_unit LIMIT 1`,
			strconv.Quote(resourceUID), hops, hops)
		app = s.queryBusinessAppByRawSQL(query)
		if app.UID != "" {
			return app
		}
	}
	return interfaces.BusinessAppContext{}
}

func (s *BusinessTopologyService) getResourceAppLabels(resourceUID string) (appName, namespace string) {
	query := fmt.Sprintf(`
		MATCH (r:K8sResource{uid: %s,is_deleted:false})
		RETURN r.K8sResource.name as name, r.K8sResource.name_namespace as ns, r.K8sResource.labels as labels LIMIT 1`,
		strconv.Quote(resourceUID))

	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		return "", ""
	}
	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return "", ""
	}

	ns, _ := getStrVal(row, "ns")
	labelsStr, _ := getStrVal(row, "labels")

	// 从 labels JSON 中提取 app.mutong.io/name 或 app.kubernetes.io/name
	name := extractBusinessAppName(labelsStr)
	return name, ns
}

func (s *BusinessTopologyService) matchBusinessAppByName(appName, namespace string) interfaces.BusinessAppContext {
	query := fmt.Sprintf(`
		MATCH (a:BusinessApp) WHERE a.BusinessApp.app_name == %s AND a.BusinessApp.namespace == %s
		RETURN a.BusinessApp.uid as uid, a.BusinessApp.app_name as app_name, a.BusinessApp.namespace as namespace, a.BusinessApp.criticality as criticality, a.BusinessApp.environment as environment, a.BusinessApp.team as team, a.BusinessApp.business_unit as business_unit LIMIT 1`,
		strconv.Quote(appName), strconv.Quote(namespace))
	return s.queryBusinessAppByRawSQL(query)
}

func extractBusinessAppName(labelsStr string) string {
	if labelsStr == "" {
		return ""
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsStr), &labels); err != nil {
		return ""
	}
	if v := labels["app.mutong.io/name"]; v != "" {
		return v
	}
	if v := labels["app.kubernetes.io/name"]; v != "" {
		return v
	}
	if v := labels["name"]; v != "" {
		return v
	}
	return ""
}

func (s *BusinessTopologyService) queryBusinessApp(resourceUID string, hops int) interfaces.BusinessAppContext {
	if hops == 0 {
		query := fmt.Sprintf(`
			MATCH (r:K8sResource{uid: %s,is_deleted:false})-[:BelongsToApp]->(a:BusinessApp)
			RETURN a.BusinessApp.uid as uid, a.BusinessApp.app_name as app_name, a.BusinessApp.namespace as namespace, a.BusinessApp.criticality as criticality, a.BusinessApp.environment as environment, a.BusinessApp.team as team, a.BusinessApp.business_unit as business_unit LIMIT 1`,
			strconv.Quote(resourceUID))
		return s.queryBusinessAppByRawSQL(query)
	}
	query := fmt.Sprintf(`
		MATCH (r:K8sResource{uid: %s,is_deleted:false})-[:OwnedBy*%d..%d]->(o:K8sResource{is_deleted:false})-[:BelongsToApp]->(a:BusinessApp)
		RETURN a.BusinessApp.uid as uid, a.BusinessApp.app_name as app_name, a.BusinessApp.namespace as namespace, a.BusinessApp.criticality as criticality, a.BusinessApp.environment as environment, a.BusinessApp.team as team, a.BusinessApp.business_unit as business_unit LIMIT 1`,
		strconv.Quote(resourceUID), hops, hops)
	return s.queryBusinessAppByRawSQL(query)
}

func (s *BusinessTopologyService) queryBusinessAppByRawSQL(query string) interfaces.BusinessAppContext {
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet.GetRowSize() == 0 {
		s.logger.Debug("No BusinessApp found")
		return interfaces.BusinessAppContext{}
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return interfaces.BusinessAppContext{}
	}

	app := interfaces.BusinessAppContext{}
	if v, err := row.GetValueByColName("uid"); err == nil {
		app.UID, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("app_name"); err == nil {
		app.AppName, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("namespace"); err == nil {
		app.Namespace, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("criticality"); err == nil {
		app.Criticality, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("environment"); err == nil {
		app.Environment, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("team"); err == nil {
		app.Team, _ = v.AsString()
	}
	if v, err := row.GetValueByColName("business_unit"); err == nil {
		app.BusinessUnit, _ = v.AsString()
	}
	app.Source = "belongs-to-app"

	return app
}
