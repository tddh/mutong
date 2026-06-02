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

	query := fmt.Sprintf(`MATCH (v:BusinessApp)%s RETURN v.BusinessApp.uid as uid, v.BusinessApp.app_name as app_name, v.BusinessApp.namespace as namespace, v.BusinessApp.criticality as criticality, v.BusinessApp.environment as environment, v.BusinessApp.team as team, v.BusinessApp.business_unit as business_unit`, whereClause)

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
		apps = append(apps, app)
	}

	return apps, nil
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
		MATCH (a:BusinessApp) WHERE a.BusinessApp.app_name == %s AND a.BusinessApp.namespace == %s AND a.BusinessApp.is_deleted == false
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
