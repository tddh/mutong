package diagnosis

import (
	"fmt"
	"strconv"
	"strings"

	nebula "github.com/vesoft-inc/nebula-go/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type TopologyQuerier struct {
	logger  interfaces.Logger
	graphDB interfaces.GraphDB
}

func NewTopologyQuerier(logger interfaces.Logger, graphDB interfaces.GraphDB) *TopologyQuerier {
	return &TopologyQuerier{
		logger:  logger,
		graphDB: graphDB,
	}
}

var coreResourceKinds = []string{
	"Pod", "Deployment", "ReplicaSet", "Service", "Ingress",
	"Node", "StatefulSet", "DaemonSet", "PersistentVolumeClaim",
}

func (q *TopologyQuerier) GetTopologySnapshot(resourceUID string) *diagnosis.TopologySnapshot {
	root, err := q.queryResourceNode(resourceUID)
	if err != nil {
		errMsg := err.Error()
		q.logger.Error("Failed to query topology", zap.Error(err), zap.String("uid", resourceUID))
		return &diagnosis.TopologySnapshot{Error: errMsg}
	}

	snapshot := &diagnosis.TopologySnapshot{
		Nodes: []diagnosis.TopologyNode{root},
		Edges: []diagnosis.TopologyEdge{},
	}

	for _, resource := range q.QueryTopologyResources(resourceUID, 2) {
		snapshot.Nodes = append(snapshot.Nodes, diagnosis.TopologyNode{
			UID:       resource.UID,
			Kind:      resource.Kind,
			Name:      resource.Name,
			Namespace: resource.Namespace,
		})
	}

	if len(snapshot.Nodes) > 1 {
		q.parseEdges(snapshot)
	}

	return snapshot
}

func (q *TopologyQuerier) QueryTopologyResources(resourceUID string, hops int) []diagnosis.ResourceRef {
	var resources []diagnosis.ResourceRef
	seen := map[string]bool{resourceUID: true}
	frontier := []string{resourceUID}

	for depth := 0; depth < hops && len(frontier) > 0; depth++ {
		nextFrontier := make([]string, 0)
		for start := 0; start < len(frontier); start += 20 {
			end := start + 20
			if end > len(frontier) {
				end = len(frontier)
			}

			adjacent := q.queryAdjacentResources(frontier[start:end], 50)
			for _, resource := range adjacent {
				if resource.UID == "" || seen[resource.UID] {
					continue
				}
				seen[resource.UID] = true
				resources = append(resources, resource)
				nextFrontier = append(nextFrontier, resource.UID)
			}
		}
		frontier = nextFrontier
	}

	return resources
}

func isCoreKind(kind string) bool {
	for _, k := range coreResourceKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (q *TopologyQuerier) parseEdges(snapshot *diagnosis.TopologySnapshot) {
	// Build UID list for edge query
	uids := make([]string, 0, len(snapshot.Nodes))
	for _, n := range snapshot.Nodes {
		uids = append(uids, strconv.Quote(n.UID))
	}
	uidList := fmt.Sprintf("[%s]", strings.Join(uids, ","))

	edgeQuery := fmt.Sprintf(
		`MATCH (src:K8sResource)-[e]-(dst:K8sResource)
		 WHERE id(src) IN %s AND id(dst) IN %s
		 RETURN type(e) AS edgeType, id(src) AS srcUID, id(dst) AS dstUID
		 LIMIT 200`,
		uidList, uidList,
	)

	resultSet, err := q.graphDB.ExecuteAndCheck(edgeQuery)
	if err != nil || resultSet == nil {
		q.logger.Debug("No edges found in topology", zap.Error(err))
		return
	}

	edgeSet := make(map[string]bool)
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}

		edgeTypeVal, _ := row.GetValueByIndex(0)
		srcVal, _ := row.GetValueByIndex(1)
		dstVal, _ := row.GetValueByIndex(2)

		if !edgeTypeVal.IsString() || !srcVal.IsString() || !dstVal.IsString() {
			continue
		}

		edgeType, _ := edgeTypeVal.AsString()
		srcUID, _ := srcVal.AsString()
		dstUID, _ := dstVal.AsString()

		if edgeType == "" || srcUID == "" || dstUID == "" {
			continue
		}

		edgeKey := fmt.Sprintf("%s->%s->%s", srcUID, dstUID, edgeType)
		if edgeSet[edgeKey] {
			continue
		}
		edgeSet[edgeKey] = true

		snapshot.Edges = append(snapshot.Edges, diagnosis.TopologyEdge{
			From: srcUID,
			To:   dstUID,
			Type: edgeType,
		})
	}
}

func (q *TopologyQuerier) QueryBusinessAppCalls(appName, namespace string) *diagnosis.BusinessAppCalls {
	result := &diagnosis.BusinessAppCalls{
		AppName:   appName,
		Namespace: namespace,
	}
	if appName == "" || namespace == "" {
		return result
	}

	upstreamQ := fmt.Sprintf(
		`MATCH (caller:BusinessApp)-[:CallsApp]->(target:BusinessApp{app_name:%s,namespace:%s})
		 RETURN caller.app_name AS name, caller.team AS team, caller.criticality AS crit
		 LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	if rs, err := q.graphDB.ExecuteAndCheck(upstreamQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, team, crit := "", "", ""
				if v, e := row.GetValueByColName("name"); e == nil {
					name, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("team"); e == nil {
					team, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("crit"); e == nil {
					crit, _ = v.AsString()
				}
				if name != "" {
					result.Upstreams = append(result.Upstreams, diagnosis.BusinessAppRef{
						AppName: name, Team: team, Criticality: crit,
					})
				}
			}
		}
	} else if err != nil {
		q.logger.Warn("QueryBusinessAppCalls upstream query failed", zap.Error(err))
	}

	downstreamQ := fmt.Sprintf(
		`MATCH (target:BusinessApp{app_name:%s,namespace:%s})-[:CallsApp]->(callee:BusinessApp)
		 RETURN callee.app_name AS name, callee.team AS team, callee.criticality AS crit
		 LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	if rs, err := q.graphDB.ExecuteAndCheck(downstreamQ); err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, team, crit := "", "", ""
				if v, e := row.GetValueByColName("name"); e == nil {
					name, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("team"); e == nil {
					team, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("crit"); e == nil {
					crit, _ = v.AsString()
				}
				if name != "" {
					result.Downstreams = append(result.Downstreams, diagnosis.BusinessAppRef{
						AppName: name, Team: team, Criticality: crit,
					})
				}
			}
		}
	} else if err != nil {
		q.logger.Warn("QueryBusinessAppCalls downstream query failed", zap.Error(err))
	}

	return result
}

func (q *TopologyQuerier) QueryWorkloadContext(podName, namespace string) *diagnosis.WorkloadContext {
	if podName == "" || namespace == "" {
		return nil
	}

	ownerQ := fmt.Sprintf(
		`MATCH (pod:K8sResource{kind:"Pod",name:%s,name_space:%s})-[:OwnedBy*1..2]->(owner:K8sResource)
		 WHERE owner.K8sResource.kind IN ["Deployment","StatefulSet","DaemonSet","CronJob"]
		 RETURN owner.K8sResource.name AS name, owner.K8sResource.kind AS kind, owner.K8sResource.name_space AS ns
		 LIMIT 1`,
		strconv.Quote(podName), strconv.Quote(namespace),
	)

	rs, err := q.graphDB.ExecuteAndCheck(ownerQ)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return nil
	}

	row, _ := rs.GetRowValuesByIndex(0)
	ownerName, _ := row.GetValueByColName("name")
	ownerKind, _ := row.GetValueByColName("kind")
	ownerNS, _ := row.GetValueByColName("ns")
	cname, _ := ownerName.AsString()
	ckind, _ := ownerKind.AsString()
	cns, _ := ownerNS.AsString()

	siblingQ := fmt.Sprintf(
		`MATCH (sibling:K8sResource{kind:"Pod",name_space:%s})-[:OwnedBy*1..2]->(owner:K8sResource{name:%s,kind:%s})
		 RETURN sibling.K8sResource.name AS name, sibling.K8sResource.is_deleted AS deleted
		 ORDER BY sibling.K8sResource.name`,
		strconv.Quote(cns), strconv.Quote(cname), strconv.Quote(ckind),
	)

	srs, err := q.graphDB.ExecuteAndCheck(siblingQ)
	if err != nil || srs == nil {
		return nil
	}

	ctx := &diagnosis.WorkloadContext{
		ControllerKind: ckind,
		ControllerName: cname,
		Namespace:      cns,
	}

	for i := 0; i < srs.GetRowSize(); i++ {
		r, err := srs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		nameVal, _ := r.GetValueByColName("name")
		delVal, _ := r.GetValueByColName("deleted")
		sName, _ := nameVal.AsString()
		isDel, _ := delVal.AsBool()

		wp := diagnosis.WorkloadPod{
			Name:      sName,
			IsAlerted: sName == podName,
			IsDeleted: isDel,
		}
		ctx.Pods = append(ctx.Pods, wp)
		ctx.TotalPods++
		if !isDel {
			ctx.HealthyPods++
		}
	}

	return ctx
}

func (q *TopologyQuerier) QuerySameNodeOrNamespace(resourceUID, nodeName, namespace string) []diagnosis.ResourceRef {
	var resources []diagnosis.ResourceRef
	seen := make(map[string]bool)

	if nodeName != "" {
		query := fmt.Sprintf(
			`MATCH (n:K8sResource{uid:%s})-[:RunsOn]->(node:K8sResource)<-[:RunsOn]-(other:K8sResource)
			 WHERE other.K8sResource.uid <> %s
			 RETURN other.K8sResource.uid, other.K8sResource.kind,
					other.K8sResource.name, other.K8sResource.name_space`,
			strconv.Quote(resourceUID), strconv.Quote(resourceUID),
		)

		resultSet, err := q.graphDB.ExecuteAndCheck(query)
		if err == nil && resultSet != nil {
			for i := 0; i < resultSet.GetRowSize(); i++ {
				row, err := resultSet.GetRowValuesByIndex(i)
				if err != nil {
					continue
				}
				uid := getStr(row, 0)
				if uid == "" || seen[uid] {
					continue
				}
				seen[uid] = true
				resources = append(resources, diagnosis.ResourceRef{
					UID:       uid,
					Kind:      getStr(row, 1),
					Name:      getStr(row, 2),
					Namespace: getStr(row, 3),
				})
			}
		}
	}

	if namespace != "" {
		query := fmt.Sprintf(
			`MATCH (n:K8sResource{uid:%s})-[:BelongsTo]->(ns:K8sResource{kind:"Namespace",name:%s})<-[:BelongsTo]-(other:K8sResource)
			 WHERE other.K8sResource.uid <> %s
			 RETURN other.K8sResource.uid, other.K8sResource.kind,
					other.K8sResource.name, other.K8sResource.name_space`,
			strconv.Quote(resourceUID), strconv.Quote(namespace), strconv.Quote(resourceUID),
		)

		resultSet, err := q.graphDB.ExecuteAndCheck(query)
		if err == nil && resultSet != nil {
			for i := 0; i < resultSet.GetRowSize(); i++ {
				row, err := resultSet.GetRowValuesByIndex(i)
				if err != nil {
					continue
				}
				uid := getStr(row, 0)
				if uid == "" || seen[uid] {
					continue
				}
				seen[uid] = true
				resources = append(resources, diagnosis.ResourceRef{
					UID:       uid,
					Kind:      getStr(row, 1),
					Name:      getStr(row, 2),
					Namespace: getStr(row, 3),
				})
			}
		}
	}

	return resources
}

func (q *TopologyQuerier) FindAffectedServices(resourceUID string) []string {
	query := fmt.Sprintf(
		`MATCH (n:K8sResource{uid:%s})<-[:SvcToPods]-(svc:K8sResource)
		 RETURN DISTINCT svc.K8sResource.name`,
		strconv.Quote(resourceUID),
	)

	resultSet, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		return nil
	}

	var services []string
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		services = append(services, getStr(row, 0))
	}

	return services
}

func (q *TopologyQuerier) FindAffectedIngresses(resourceUID string) []string {
	query := fmt.Sprintf(
		`MATCH (n:K8sResource{uid:%s})-[*1..2]-(ingress:K8sResource{kind:"Ingress"})
		 RETURN DISTINCT ingress.K8sResource.name`,
		strconv.Quote(resourceUID),
	)

	resultSet, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		return nil
	}

	var ingresses []string
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		ingresses = append(ingresses, getStr(row, 0))
	}

	return ingresses
}

func getStr(row *nebula.Record, idx int) string {
	val, err := row.GetValueByIndex(idx)
	if err != nil || !val.IsString() {
		return ""
	}
	s, _ := val.AsString()
	return s
}

func (q *TopologyQuerier) queryResourceNode(resourceUID string) (diagnosis.TopologyNode, error) {
	query := fmt.Sprintf(
		`MATCH (n:K8sResource{uid:%s,is_deleted:false})
		 RETURN n.K8sResource.uid, n.K8sResource.kind,
		 		n.K8sResource.name, n.K8sResource.name_space,
		 		n.K8sResource.is_deleted LIMIT 1`,
		strconv.Quote(resourceUID),
	)

	resultSet, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		if err != nil {
			q.logger.Error("queryResourceNode failed", zap.String("uid", resourceUID), zap.String("query", query), zap.Error(err))
			return diagnosis.TopologyNode{}, err
		}
		return diagnosis.TopologyNode{}, fmt.Errorf("resource not found")
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return diagnosis.TopologyNode{}, err
	}

	node := diagnosis.TopologyNode{
		UID:       getStr(row, 0),
		Kind:      getStr(row, 1),
		Name:      getStr(row, 2),
		Namespace: getStr(row, 3),
	}
	if deletedVal, err := row.GetValueByIndex(4); err == nil && deletedVal.IsBool() {
		node.IsDeleted, _ = deletedVal.AsBool()
	}

	return node, nil
}

func (q *TopologyQuerier) queryAdjacentResources(resourceUIDs []string, limit int) []diagnosis.ResourceRef {
	if len(resourceUIDs) == 0 {
		return nil
	}

	quoted := make([]string, 0, len(resourceUIDs))
	for _, uid := range resourceUIDs {
		quoted = append(quoted, strconv.Quote(uid))
	}

	query := fmt.Sprintf(
		`MATCH (src:K8sResource)-[e]-(dst:K8sResource{is_deleted:false})
		 WHERE src.K8sResource.uid IN [%s]
		 RETURN DISTINCT dst.K8sResource.uid, dst.K8sResource.kind,
		 		dst.K8sResource.name, dst.K8sResource.name_space
		 LIMIT %d`,
		strings.Join(quoted, ","), limit,
	)

	resultSet, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil {
		q.logger.Error("Failed to query topology resources", zap.String("query", query), zap.Error(err))
		return nil
	}

	resources := make([]diagnosis.ResourceRef, 0, resultSet.GetRowSize())
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}

		kind := getStr(row, 1)
		if !isCoreKind(kind) {
			continue
		}

		resources = append(resources, diagnosis.ResourceRef{
			UID:       getStr(row, 0),
			Kind:      kind,
			Name:      getStr(row, 2),
			Namespace: getStr(row, 3),
		})
	}

	return resources
}

// ResolveUID 通过 kind + name + namespace 解析资源的 UID
func (q *TopologyQuerier) ResolveUID(kind, name, namespace string) string {
	var query string
	if kind == "Node" {
		query = fmt.Sprintf(
			`MATCH (n:K8sResource{kind:'Node',name:%s})
			 RETURN n.K8sResource.uid LIMIT 1`,
			strconv.Quote(name),
		)
	} else if namespace != "" {
		query = fmt.Sprintf(
			`MATCH (n:K8sResource{kind:%s,name:%s,name_space:%s})
			 RETURN n.K8sResource.uid LIMIT 1`,
			strconv.Quote(kind), strconv.Quote(name), strconv.Quote(namespace),
		)
	} else {
		query = fmt.Sprintf(
			`MATCH (n:K8sResource{kind:%s,name:%s})
			 RETURN n.K8sResource.uid LIMIT 1`,
			strconv.Quote(kind), strconv.Quote(name),
		)
	}

	resultSet, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		q.logger.Debug("ResolveUID: resource not found",
			zap.String("kind", kind), zap.String("name", name),
			zap.String("namespace", namespace))
		return ""
	}

	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return ""
	}

	val, err := row.GetValueByIndex(0)
	if err != nil || !val.IsString() {
		return ""
	}

	uid, _ := val.AsString()
	return uid
}

// BusinessImpactContext 提供给 LLM 推理的完整业务拓扑上下文
type BusinessImpactContext struct {
	// DirectBusinessApps 告警资源直接归属的业务应用列表
	DirectBusinessApps []BusinessAppMeta

	// DownstreamCallChain 2跳下游调用链路（平铺列表，每条是一对 caller→callee）
	DownstreamCallChain []FlatBusinessCall

	// AllBusinessAppMetadata 涉及的所有业务应用元数据（"appName|namespace" → meta）
	AllBusinessAppMetadata map[string]BusinessAppMeta
}

// BusinessAppMeta 业务应用元数据
type BusinessAppMeta struct {
	AppName      string
	Namespace    string
	Criticality  string
	Team         string
	BusinessUnit string
}

// FlatBusinessCall 平铺的调用链记录
type FlatBusinessCall struct {
	Caller      string
	CallerNS    string
	Callee      string
	CalleeNS    string
	HopDistance int // 距离直接业务应用的跳数（1=直接业务应用→1跳下游, 2=1跳下游→2跳下游）
}

// downstreamAppRef 下游业务应用引用（内部使用）
type downstreamAppRef struct {
	AppName      string
	Namespace    string
	Criticality  string
	Team         string
	BusinessUnit string
}

// BuildBusinessImpactContext 构建 LLM 推理所需的业务影响上下文
// 1. 查询资源 → BelongsToApp(或owner链) → 直接 BusinessApp
// 2. 对每个直接 BusinessApp，沿 CallsApp 边 BFS 2 跳查询下游
// 3. 查询所有涉及 BusinessApp 的元数据
func (q *TopologyQuerier) BuildBusinessImpactContext(uid string, alertBizCtx alert_models.BusinessContext) *BusinessImpactContext {
	ctx := &BusinessImpactContext{
		AllBusinessAppMetadata: make(map[string]BusinessAppMeta),
	}

	// Step 1: 确定直接影响的业务应用
	directApps := q.resolveDirectBusinessApps(uid, alertBizCtx)
	ctx.DirectBusinessApps = directApps

	if len(directApps) == 0 {
		return ctx
	}

	// 填充直接业务应用的元数据
	seen := make(map[string]bool)
	for _, app := range directApps {
		key := app.AppName + "|" + app.Namespace
		seen[key] = true
		ctx.AllBusinessAppMetadata[key] = BusinessAppMeta{
			AppName:      app.AppName,
			Namespace:    app.Namespace,
			Criticality:  app.Criticality,
			Team:         app.Team,
			BusinessUnit: app.BusinessUnit,
		}
	}

	// Step 2: 查询直接调用关系（上游+下游）
	// 直接影响 = 直接调用 kafka 的应用（上游） + kafka 直接调用的应用（下游）
	for _, app := range directApps {
		// 查询上游调用方（谁调用 kafka）
		upstreams := q.queryUpstreamBusinessApps(app.AppName, app.Namespace)
		for _, us := range upstreams {
			key := us.AppName + "|" + us.Namespace
			if seen[key] {
				continue
			}
			seen[key] = true

			call := FlatBusinessCall{
				Caller:      us.AppName,
				CallerNS:    us.Namespace,
				Callee:      app.AppName,
				CalleeNS:    app.Namespace,
				HopDistance: 1, // 直接影响
			}
			ctx.DownstreamCallChain = append(ctx.DownstreamCallChain, call)

			ctx.AllBusinessAppMetadata[key] = BusinessAppMeta(us)
		}

		// 查询下游被调用方（kafka 调用谁）
		downstreams := q.queryDownstreamBusinessApps(app.AppName, app.Namespace)
		for _, ds := range downstreams {
			key := ds.AppName + "|" + ds.Namespace
			if seen[key] {
				continue
			}
			seen[key] = true

			call := FlatBusinessCall{
				Caller:      app.AppName,
				CallerNS:    app.Namespace,
				Callee:      ds.AppName,
				CalleeNS:    ds.Namespace,
				HopDistance: 1, // 直接影响
			}
			ctx.DownstreamCallChain = append(ctx.DownstreamCallChain, call)

			ctx.AllBusinessAppMetadata[key] = BusinessAppMeta(ds)
		}
	}

	// Step 3: 查询第二层调用关系（上游的上游 + 下游的下游）
	// 间接影响 = 上游的上游 + 下游的下游
	for _, app := range directApps {
		// 查询上游的上游
		upstreams := q.queryUpstreamBusinessApps(app.AppName, app.Namespace)
		for _, us := range upstreams {
			upstreamOfUpstream := q.queryUpstreamBusinessApps(us.AppName, us.Namespace)
			for _, uou := range upstreamOfUpstream {
				key := uou.AppName + "|" + uou.Namespace
				if seen[key] {
					continue
				}
				seen[key] = true

				call := FlatBusinessCall{
					Caller:      uou.AppName,
					CallerNS:    uou.Namespace,
					Callee:      us.AppName,
					CalleeNS:    us.Namespace,
					HopDistance: 2, // 间接影响
				}
				ctx.DownstreamCallChain = append(ctx.DownstreamCallChain, call)

				ctx.AllBusinessAppMetadata[key] = BusinessAppMeta(uou)
			}
		}

		// 查询下游的下游
		downstreams := q.queryDownstreamBusinessApps(app.AppName, app.Namespace)
		for _, ds := range downstreams {
			downstreamOfDownstream := q.queryDownstreamBusinessApps(ds.AppName, ds.Namespace)
			for _, dod := range downstreamOfDownstream {
				key := dod.AppName + "|" + dod.Namespace
				if seen[key] {
					continue
				}
				seen[key] = true

				call := FlatBusinessCall{
					Caller:      ds.AppName,
					CallerNS:    ds.Namespace,
					Callee:      dod.AppName,
					CalleeNS:    dod.Namespace,
					HopDistance: 2, // 间接影响
				}
				ctx.DownstreamCallChain = append(ctx.DownstreamCallChain, call)

				ctx.AllBusinessAppMetadata[key] = BusinessAppMeta(dod)
			}
		}
	}

	return ctx
}

// queryUpstreamBusinessApps 查询指定 BusinessApp 的上游调用方（谁调用它）
func (q *TopologyQuerier) queryUpstreamBusinessApps(appName, namespace string) []downstreamAppRef {
	upstreamQ := fmt.Sprintf(
		`MATCH (caller:BusinessApp)-[:CallsApp]->(target:BusinessApp{app_name:%s,namespace:%s})
		 RETURN caller.BusinessApp.app_name AS name, caller.BusinessApp.team AS team, caller.BusinessApp.criticality AS crit
		 LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	var result []downstreamAppRef
	rs, err := q.graphDB.ExecuteAndCheck(upstreamQ)
	if err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, team, crit := "", "", ""
				if v, e := row.GetValueByColName("name"); e == nil {
					name, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("team"); e == nil {
					team, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("crit"); e == nil {
					crit, _ = v.AsString()
				}
				if name != "" {
					result = append(result, downstreamAppRef{
						AppName: name, Team: team, Criticality: crit,
					})
				}
			}
		}
	}
	return result
}

// queryDownstreamBusinessApps 查询指定 BusinessApp 的 1 跳下游
func (q *TopologyQuerier) queryDownstreamBusinessApps(appName, namespace string) []downstreamAppRef {
	downstreamQ := fmt.Sprintf(
		`MATCH (target:BusinessApp{app_name:%s,namespace:%s})-[:CallsApp]->(callee:BusinessApp)
		 RETURN callee.BusinessApp.app_name AS name, callee.BusinessApp.team AS team, callee.BusinessApp.criticality AS crit
		 LIMIT 20`,
		strconv.Quote(appName), strconv.Quote(namespace),
	)

	var result []downstreamAppRef
	rs, err := q.graphDB.ExecuteAndCheck(downstreamQ)
	if err == nil && rs != nil {
		for i := 0; i < rs.GetRowSize(); i++ {
			if row, err := rs.GetRowValuesByIndex(i); err == nil {
				name, team, crit := "", "", ""
				if v, e := row.GetValueByColName("name"); e == nil {
					name, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("team"); e == nil {
					team, _ = v.AsString()
				}
				if v, e := row.GetValueByColName("crit"); e == nil {
					crit, _ = v.AsString()
				}
				if name != "" {
					result = append(result, downstreamAppRef{
						AppName: name, Team: team, Criticality: crit,
					})
				}
			}
		}
	}
	return result
}

// resolveDirectBusinessApps 从资源 UID 和告警富化的业务上下文中确定直接影响的业务应用
func (q *TopologyQuerier) resolveDirectBusinessApps(uid string, alertBizCtx alert_models.BusinessContext) []BusinessAppMeta {
	var apps []BusinessAppMeta

	// 优先使用告警富化阶段注入的业务上下文
	if alertBizCtx.AppName != "" {
		apps = append(apps, BusinessAppMeta{
			AppName:      alertBizCtx.AppName,
			Namespace:    alertBizCtx.Namespace,
			Criticality:  alertBizCtx.Criticality,
			Team:         alertBizCtx.Team,
			BusinessUnit: alertBizCtx.BusinessUnit,
		})
		return apps
	}

	if uid == "" {
		return nil
	}

	query := fmt.Sprintf(
		`MATCH (r:K8sResource{uid:%s})-[:BelongsToApp]->(biz:BusinessApp) RETURN biz.app_name AS name, biz.namespace AS ns, biz.criticality AS crit, biz.team AS team, biz.business_unit AS unit LIMIT 5`,
		strconv.Quote(uid),
	)

	rs, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil {
		q.logger.Warn("resolveDirectBusinessApps BelongsToApp query failed", zap.Error(err))
		return q.resolveDirectBusinessAppsViaOwner(uid)
	}
	if rs == nil || rs.GetRowSize() == 0 {
		return q.resolveDirectBusinessAppsViaOwner(uid)
	}

	for i := 0; i < rs.GetRowSize(); i++ {
		row, err := rs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		name := getStr(row, 0)
		ns := getStr(row, 1)
		crit := getStr(row, 2)
		team := getStr(row, 3)
		unit := getStr(row, 4)
		if name == "" {
			continue
		}
		apps = append(apps, BusinessAppMeta{
			AppName: name, Namespace: ns, Criticality: crit, Team: team, BusinessUnit: unit,
		})
	}
	return apps
}

// NodeMetrics holds key resource utilization metrics for a Kubernetes Node.
type NodeMetrics struct {
	CPUUtilization    float64
	MemoryUtilization float64
	DiskUtilization   float64
}

// MaxUtilization returns the highest utilization among CPU, memory, and disk.
func (m *NodeMetrics) MaxUtilization() float64 {
	max := m.CPUUtilization
	if m.MemoryUtilization > max {
		max = m.MemoryUtilization
	}
	if m.DiskUtilization > max {
		max = m.DiskUtilization
	}
	return max / 100.0
}

// TopologyExpandResult 拓扑展开结果
type TopologyExpandResult struct {
	UID       string `json:"uid"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
}

// ExpandPathFlags 控制哪些拓扑路径展开
type ExpandPathFlags struct {
	SameNode         bool
	SameOwner        bool
	SameService      bool
	SameBizApp       bool
	UpstreamBizApp   bool
	DownstreamBizApp bool
}

// ExpandRelatedResourceUIDs 从告警资源 UID 出发，按双层拓扑展开相关邻居资源 UID。
func (q *TopologyQuerier) ExpandRelatedResourceUIDs(
	resourceUID string,
	nodeName string,
	namespace string,
	ownerKind string,
	ownerName string,
	businessAppName string,
	businessAppNS string,
	maxPerPath int,
	flags ExpandPathFlags,
) []TopologyExpandResult {
	if maxPerPath <= 0 {
		maxPerPath = 20
	}
	seen := make(map[string]bool)
	var results []TopologyExpandResult

	addResult := func(r TopologyExpandResult) {
		if r.UID == "" || seen[r.UID] {
			return
		}
		seen[r.UID] = true
		results = append(results, r)
	}

	// Path 1: same_node
	if flags.SameNode && resourceUID != "" && nodeName != "" {
		query := fmt.Sprintf(
			`MATCH (pod:K8sResource{kind:'Pod',is_deleted:false})-[:RunsOn]->(n:K8sResource{kind:'Node',name:%s})
			 WHERE pod.K8sResource.uid <> %s
			 RETURN pod.K8sResource.uid AS uid, pod.K8sResource.kind AS kind, pod.K8sResource.name AS name, pod.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(nodeName), strconv.Quote(resourceUID), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "same_node",
					})
				}
			}
		}
	}

	// Path 2: same_owner
	if flags.SameOwner && namespace != "" && ownerKind != "" && ownerName != "" {
		query := fmt.Sprintf(
			`MATCH (owner:K8sResource{kind:%s,name:%s,name_space:%s,is_deleted:false})<-[:OwnedBy*1..2]-(sibling:K8sResource{kind:'Pod',is_deleted:false})
			 RETURN sibling.K8sResource.uid AS uid, sibling.K8sResource.kind AS kind, sibling.K8sResource.name AS name, sibling.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(ownerKind), strconv.Quote(ownerName), strconv.Quote(namespace), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "same_owner",
					})
				}
			}
		}
	}

	// Path 3: same_service
	if flags.SameService && resourceUID != "" {
		query := fmt.Sprintf(
			`MATCH (svc:K8sResource{kind:'Service'})-[:SvcToPods]->(pod:K8sResource{kind:'Pod',is_deleted:false})
			 WHERE pod.K8sResource.uid = %s
			 MATCH (svc)-[:SvcToPods]->(peer:K8sResource{kind:'Pod',is_deleted:false})
			 WHERE peer.K8sResource.uid <> %s
			 RETURN DISTINCT peer.K8sResource.uid AS uid, peer.K8sResource.kind AS kind, peer.K8sResource.name AS name, peer.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(resourceUID), strconv.Quote(resourceUID), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "same_service",
					})
				}
			}
		}
	}

	// Path 4: same_bizapp
	if flags.SameBizApp && resourceUID != "" && businessAppName != "" && businessAppNS != "" {
		query := fmt.Sprintf(
			`MATCH (biz:BusinessApp{app_name:%s,namespace:%s})<-[:BelongsToApp]-(r:K8sResource{is_deleted:false})
			 WHERE r.K8sResource.uid <> %s
			 RETURN r.K8sResource.uid AS uid, r.K8sResource.kind AS kind, r.K8sResource.name AS name, r.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(businessAppName), strconv.Quote(businessAppNS), strconv.Quote(resourceUID), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "same_bizapp",
					})
				}
			}
		}
	}

	// Path 5: upstream_bizapp
	if flags.UpstreamBizApp && businessAppName != "" && businessAppNS != "" {
		query := fmt.Sprintf(
			`MATCH (caller:BusinessApp)-[:CallsApp]->(target:BusinessApp{app_name:%s,namespace:%s})
			 MATCH (caller)<-[:BelongsToApp]-(r:K8sResource{is_deleted:false})
			 RETURN r.K8sResource.uid AS uid, r.K8sResource.kind AS kind, r.K8sResource.name AS name, r.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(businessAppName), strconv.Quote(businessAppNS), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "upstream_bizapp",
					})
				}
			}
		}
	}

	// Path 6: downstream_bizapp
	if flags.DownstreamBizApp && businessAppName != "" && businessAppNS != "" {
		query := fmt.Sprintf(
			`MATCH (target:BusinessApp{app_name:%s,namespace:%s})-[:CallsApp]->(callee:BusinessApp)
			 MATCH (callee)<-[:BelongsToApp]-(r:K8sResource{is_deleted:false})
			 RETURN r.K8sResource.uid AS uid, r.K8sResource.kind AS kind, r.K8sResource.name AS name, r.K8sResource.name_space AS ns
			 LIMIT %d`,
			strconv.Quote(businessAppName), strconv.Quote(businessAppNS), maxPerPath,
		)
		if rs, err := q.graphDB.ExecuteAndCheck(query); err == nil && rs != nil {
			for i := 0; i < rs.GetRowSize(); i++ {
				if row, err := rs.GetRowValuesByIndex(i); err == nil {
					addResult(TopologyExpandResult{
						UID: getStr(row, 0), Kind: getStr(row, 1),
						Name: getStr(row, 2), Namespace: getStr(row, 3),
						Path: "downstream_bizapp",
					})
				}
			}
		}
	}

	return results
}

// buildExpandFlags determines which topology paths to expand based on alert context.
func buildExpandFlags(alert *alert_models.ProcessedAlert, nodeMetrics *NodeMetrics) ExpandPathFlags {
	return ExpandPathFlags{
		SameNode:    isNodeAlert(alert) || (nodeMetrics != nil && nodeMetrics.MaxUtilization() > 0.8),
		SameOwner:   alert.OwnerKind != "",
		SameService: isTrafficAlert(alert),
		SameBizApp:  alert.BusinessContext.AppName != "",
		UpstreamBizApp: alert.BusinessContext.ServiceType == "middleware" ||
			(isServiceLevel(alert) && alert.Routing.Severity == "critical"),
		DownstreamBizApp: alert.BusinessCalls != nil && len(alert.BusinessCalls.Downstreams) > 0,
	}
}

func isNodeAlert(a *alert_models.ProcessedAlert) bool {
	return a.Labels != nil && strings.Contains(a.Labels["alertname"], "Node")
}

func isTrafficAlert(a *alert_models.ProcessedAlert) bool {
	if a.Labels == nil {
		return false
	}
	an := strings.ToLower(a.Labels["alertname"])
	return strings.Contains(an, "error") || strings.Contains(an, "latency") ||
		strings.Contains(an, "timeout") || strings.Contains(an, "5xx")
}

func isServiceLevel(a *alert_models.ProcessedAlert) bool {
	rt := a.ResourceType
	return rt == "Deployment" || rt == "Service" || rt == "StatefulSet"
}

// resolveDirectBusinessAppsViaOwner 沿 owner 链查找业务应用归属
func (q *TopologyQuerier) resolveDirectBusinessAppsViaOwner(uid string) []BusinessAppMeta {
	if uid == "" {
		return nil
	}
	query := fmt.Sprintf(
		`MATCH p=(r:K8sResource{uid:%s})-[:OwnedBy*0..5]->(owner:K8sResource)-[:BelongsToApp]->(biz:BusinessApp) RETURN biz.app_name AS name, biz.namespace AS ns, biz.criticality AS crit, biz.team AS team, biz.business_unit AS unit LIMIT 3`,
		strconv.Quote(uid),
	)
	rs, err := q.graphDB.ExecuteAndCheck(query)
	if err != nil || rs == nil {
		return nil
	}
	var apps []BusinessAppMeta
	for i := 0; i < rs.GetRowSize(); i++ {
		row, err := rs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		name := getStr(row, 0)
		if name == "" {
			continue
		}
		apps = append(apps, BusinessAppMeta{
			AppName: name, Namespace: getStr(row, 1),
			Criticality: getStr(row, 2), Team: getStr(row, 3), BusinessUnit: getStr(row, 4),
		})
	}
	return apps
}
