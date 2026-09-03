package services

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models"
	"github.com/valyala/fastjson"
	nebula "github.com/vesoft-inc/nebula-go/v3/nebula"
	"go.uber.org/zap"
)

// 懒加载缓存的拓扑遍历边类型（排除 BelongsToLabel、Events）
var (
	cachedEdgeTypes string
	edgeTypesOnce   sync.Once
	edgeTypesErr    error
)

// getTraversalEdgeTypes 从 Nebula 动态获取边类型并排除噪音边
func (d *K8sResoureService) getTraversalEdgeTypes() string {
	edgeTypesOnce.Do(func() {
		resultSet, err := d.graphDB.ExecuteAndCheck("SHOW EDGES;")
		if err != nil {
			edgeTypesErr = err
			d.logger.Warn("getTraversalEdgeTypes: SHOW EDGES failed, using fallback", zap.Error(err))
			return
		}
		var types []string
		rows := resultSet.GetRows()
		for _, row := range rows {
			name := string(row.Values[0].GetSVal())
			if name != "BelongsToLabel" && name != "Events" {
				types = append(types, name)
			}
		}
		cachedEdgeTypes = strings.Join(types, ",")
		d.logger.Info("getTraversalEdgeTypes: loaded", zap.Int("count", len(types)))
	})
	if edgeTypesErr != nil || cachedEdgeTypes == "" {
		return "BelongsTo,RunsOn,OwnedBy,ServiceAccount,MountsConfig,MountsPVC,MountsSecret,SvcToPods,EpToPods"
	}
	return cachedEdgeTypes
}

func (d *K8sResoureService) Get(reqCtx interfaces.RequestContext) (interface{}, error) {
	traceID := reqCtx.TraceID

	query := `MATCH (v:K8sResource{is_deleted: false}) RETURN v.K8sResource.name as name ,v.K8sResource.kind as kind,v.K8sResource.api_version as api_version ,v.K8sResource.api_group as group,v.K8sResource.uid as uid ,v.K8sResource.labels as labels,v.K8sResource.name_space as name_space`

	d.logger.Debug("query all resources", zap.String("X-Trace-ID", traceID), zap.String("query", query))
	// 执行查询
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("Failed to execute query", zap.String("X-Trace-ID", traceID), zap.String("query", query), zap.Error(err))
		return nil, err
	}

	// 解析结果
	rows := resultSet.GetRows()
	if len(rows) == 0 {
		d.logger.Debug("No resource found with the given name", zap.String("X-Trace-ID", traceID))
		return nil, errors.New("no resource found with the given name")
	}

	// 构建 JSON 输出
	var result []models.K8sResource
	for _, row := range rows {

		//labels, _ := strconv.Unquote(string(row.Values[5].GetSVal()))
		//resourceDefine, _ := strconv.Unquote(string(row.Values[7].GetSVal()))

		// 使用 parseJSONString 方法解析字符串
		labels, _ := parseJSONString(string(row.Values[5].GetSVal()))
		resourceDefine, _ := parseJSONString(string(row.Values[7].GetSVal()))
		resource := models.K8sResource{
			Name:           string(row.Values[0].GetSVal()),
			Kind:           string(row.Values[1].GetSVal()),
			APIVersion:     string(row.Values[2].GetSVal()),
			Group:          string(row.Values[3].GetSVal()),
			Uid:            string(row.Values[4].GetSVal()),
			Labels:         labels,
			NameSpace:      string(row.Values[6].GetSVal()),
			ResourceDefine: resourceDefine,
		}
		result = append(result, resource)
	}

	jsonResult := d.mapToJson(result)

	//d.roleSvc.GetRoleByID(123)

	return string(jsonResult), nil
}

func parseJSONString(s string) (string, error) {
	var p fastjson.Parser
	v, err := p.Parse(s)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// GetAllResources 获取K8s资源节点列表（分页）
// 用 GO FROM namespace REVERSELY 替代 LOOKUP，避免 storaged OOM
func (d *K8sResoureService) countResourcesByKind(kind string) int {
	if kind == "" || kind == "all" {
		return d.countAllResources()
	}
	query := fmt.Sprintf(
		`LOOKUP ON K8sResource WHERE K8sResource.kind == %s AND K8sResource.is_deleted == false
		 YIELD K8sResource.name AS name | YIELD count(*) AS cnt`,
		strconv.Quote(kind),
	)
	rs, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return 0
	}
	row, err := rs.GetRowValuesByIndex(0)
	if err != nil {
		return 0
	}
	val, err := row.GetValueByColName("cnt")
	if err != nil {
		return 0
	}
	cnt, err := val.AsInt()
	if err != nil {
		return 0
	}
	return int(cnt)
}

func (d *K8sResoureService) countAllResources() int {
	query := `LOOKUP ON K8sResource WHERE K8sResource.is_deleted == false YIELD K8sResource.name AS name | YIELD count(*) AS cnt`
	rs, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return 0
	}
	row, err := rs.GetRowValuesByIndex(0)
	if err != nil {
		return 0
	}
	val, err := row.GetValueByColName("cnt")
	if err != nil {
		return 0
	}
	cnt, err := val.AsInt()
	if err != nil {
		return 0
	}
	return int(cnt)
}

// clusterScopedNamespace 是前端"🌐 集群级"下拉项与后端约定的哨兵值。
// 下划线在 K8s 命名空间名（DNS-1123 label）中非法，故永不与真实命名空间冲突。
const clusterScopedNamespace = "__cluster__"

func (d *K8sResoureService) GetAllResources(reqCtx interfaces.RequestContext) (interface{}, error) {
	traceID := reqCtx.TraceID

	page := 1
	pageSize := 500
	if pv := reqCtx.Query("page"); pv != "" {
		if v, err := strconv.Atoi(pv); err == nil && v > 0 {
			page = v
		}
	}
	if psize := reqCtx.Query("pageSize"); psize != "" {
		if v, err := strconv.Atoi(psize); err == nil && v > 0 && v <= 5000 {
			pageSize = v
		}
	}

	namespace := reqCtx.Query("namespace")
	kind := reqCtx.Query("kind")

	cacheKeyPlain := fmt.Sprintf("nodes:%s:%s:%d:%d", namespace, kind, page, pageSize)
	cacheKey := fmt.Sprintf("nodes:%x", sha256.Sum256([]byte(cacheKeyPlain)))
	if d.resourceCache != nil {
		if cached, ok := d.resourceCache.Get(cacheKey); ok {
			return cached, nil
		}
	}

	var allRows []*nebula.Row

	if namespace == clusterScopedNamespace {
		// 集群级资源（Node/Namespace/PersistentVolume/ClusterRole/CRD/CiliumIdentity 等）：
		// name_space 为空串、没有 BelongsTo 边，无法通过命名空间遍历得到，走独立 MATCH。
		rows, err := d.queryClusterScopedResources(traceID)
		if err != nil {
			d.logger.Warn("GetAllResources: cluster-scoped query failed", zap.String("X-Trace-ID", traceID), zap.Error(err))
			return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
		}
		allRows = rows
	} else {
		var nsVIDs []string

		if namespace == "" || namespace == "all" {
			// 获取所有 Namespace 的 VID，批量 GO FROM
			allVIDs, err := d.getAllNamespaceVIDs(traceID)
			if err != nil {
				d.logger.Warn("GetAllResources: failed to get all namespace VIDs", zap.Error(err))
				return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
			}
			nsVIDs = allVIDs
		} else {
			nsVID, err := d.getNamespaceVID(namespace, traceID)
			if err != nil || nsVID == "" {
				d.logger.Warn("GetAllResources: namespace VID not found", zap.String("ns", namespace), zap.Error(err))
				return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
			}
			nsVIDs = []string{nsVID}
		}

		if len(nsVIDs) == 0 {
			return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
		}

		vidListJSON := listToJSON(nsVIDs)
		vidList := strings.Trim(vidListJSON, "[]")

		if kind == "Node" {
			nodeQuery := `MATCH (v:K8sResource{kind:"Node", is_deleted:false}) RETURN v.K8sResource.name AS name, v.K8sResource.kind AS kind, v.K8sResource.api_version AS api_version, v.K8sResource.api_group AS group, id(v) AS uid, v.K8sResource.name_space AS name_space`
			d.logger.Debug("GetAllResources MATCH Node", zap.String("X-Trace-ID", traceID), zap.String("query", nodeQuery))
			resultSet, err := d.graphDB.ExecuteAndCheck(nodeQuery)
			if err != nil {
				d.logger.Warn("GetAllResources MATCH Node failed", zap.Error(err))
				return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
			}
			allRows = resultSet.GetRows()
		} else {
			nodeQuery := fmt.Sprintf(
				`GO FROM %s OVER BelongsTo REVERSELY
		 WHERE $$.K8sResource.is_deleted == false
		 YIELD $$.K8sResource.name AS name, $$.K8sResource.kind AS kind,
		       $$.K8sResource.api_version AS api_version, $$.K8sResource.api_group AS group,
		       id($$) AS uid, $$.K8sResource.name_space AS name_space`,
				vidList,
			)
			d.logger.Debug("GetAllResources GO FROM REVERSELY", zap.String("X-Trace-ID", traceID), zap.String("query", nodeQuery))
			resultSet, err := d.graphDB.ExecuteAndCheck(nodeQuery)
			if err != nil {
				d.logger.Warn("GetAllResources GO failed, returning empty", zap.String("X-Trace-ID", traceID), zap.Error(err))
				return map[string]interface{}{"nodes": []map[string]interface{}{}, "edges": []map[string]interface{}{}, "totalCount": 0}, nil
			}
			allRows = resultSet.GetRows()

			// "所有命名空间" 时并入集群级资源，使 all 名副其实；失败仅告警，不影响命名空间资源返回
			if namespace == "" || namespace == "all" {
				csRows, csErr := d.queryClusterScopedResources(traceID)
				if csErr != nil {
					d.logger.Warn("GetAllResources: cluster-scoped union failed", zap.String("X-Trace-ID", traceID), zap.Error(csErr))
				} else {
					allRows = append(allRows, csRows...)
				}
			}
		}
	}

	var matchedRows []*nebula.Row
	for _, row := range allRows {
		if kind != "" && kind != "all" {
			if string(row.Values[1].GetSVal()) != kind {
				continue
			}
		}
		matchedRows = append(matchedRows, row)
	}

	// totalCount 必须是本次查询范围（命名空间 + kind 过滤后）去重后的真实资源数。
	// 不能用 countResourcesByKind(kind)：那是全集群按 kind 计数、与命名空间无关，
	// 会让 totalCount 虚高（如 k8s-gpt 实际 124 个却报 2476），进而 totalPages 虚高、分页错乱。
	uidSeen := make(map[string]bool)
	for _, row := range matchedRows {
		if len(row.Values) > 4 {
			uidSeen[string(row.Values[4].GetSVal())] = true
		}
	}
	totalCount := len(uidSeen)
	skip := (page - 1) * pageSize
	var pageRows []*nebula.Row
	if skip < len(matchedRows) {
		end := skip + pageSize
		if end > len(matchedRows) {
			end = len(matchedRows)
		}
		pageRows = matchedRows[skip:end]
	}

	var nodes []map[string]interface{}
	nodeIDs := make(map[string]bool)

	for _, row := range pageRows {
		resourceUID := string(row.Values[4].GetSVal())
		if !nodeIDs[resourceUID] {
			node := make(map[string]interface{})
			node["id"] = resourceUID
			node["label"] = string(row.Values[0].GetSVal())
			node["kind"] = string(row.Values[1].GetSVal())
			node["apiVersion"] = string(row.Values[2].GetSVal())
			node["group"] = string(row.Values[3].GetSVal())
			node["namespace"] = string(row.Values[5].GetSVal())

			style := make(map[string]interface{})
			style["fill"] = getColorByKind(string(row.Values[1].GetSVal()))
			node["style"] = style

			nodes = append(nodes, node)
			nodeIDs[resourceUID] = true
		}
	}

	// 该命名空间的完整 kind 列表（基于分页前的全量 allRows，不受当前页限制），
	// 供前端"资源类型"面板使用——否则面板只能看到当前页 20 个节点里出现的 kind，与集群实际对不上。
	kindSet := make(map[string]bool)
	for _, row := range allRows {
		if len(row.Values) < 2 {
			continue
		}
		if k := string(row.Values[1].GetSVal()); k != "" && k != "Label" {
			kindSet[k] = true
		}
	}
	kindsList := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kindsList = append(kindsList, k)
	}
	sort.Strings(kindsList)

	result := map[string]interface{}{
		"nodes":      nodes,
		"edges":      []map[string]interface{}{},
		"totalCount": totalCount,
		"kinds":      kindsList,
	}

	if d.resourceCache != nil {
		_ = d.resourceCache.Set(cacheKey, result)
	}

	return result, nil
}

// SuggestResources 搜索建议：按名称/UID模糊匹配，返回匹配的前10条
func (d *K8sResoureService) SuggestResources(reqCtx interfaces.RequestContext) (interface{}, error) {
	traceID := reqCtx.TraceID
	q := strings.ToLower(strings.TrimSpace(reqCtx.Query("q")))
	if q == "" {
		return []map[string]interface{}{}, nil
	}
	namespace := reqCtx.Query("namespace")

	cacheKeyPlain := fmt.Sprintf("suggest:%s:%s", namespace, q)
	cacheKey := fmt.Sprintf("suggest:%x", sha256.Sum256([]byte(cacheKeyPlain)))
	if d.resourceCache != nil {
		if cached, ok := d.resourceCache.Get(cacheKey); ok {
			return cached, nil
		}
	}

	var nsVIDs []string
	if namespace == "" || namespace == "all" {
		allVIDs, err := d.getAllNamespaceVIDs(traceID)
		if err != nil {
			d.logger.Warn("SuggestResources: failed to get all namespace VIDs", zap.Error(err))
			return []map[string]interface{}{}, nil
		}
		nsVIDs = allVIDs
	} else {
		nsVID, err := d.getNamespaceVID(namespace, traceID)
		if err != nil || nsVID == "" {
			d.logger.Warn("SuggestResources: namespace VID not found", zap.String("ns", namespace), zap.Error(err))
			return []map[string]interface{}{}, nil
		}
		nsVIDs = []string{nsVID}
	}
	if len(nsVIDs) == 0 {
		return []map[string]interface{}{}, nil
	}

	vidListJSON := listToJSON(nsVIDs)
	vidList := strings.Trim(vidListJSON, "[]")
	nodeQuery := fmt.Sprintf(
		`GO FROM %s OVER BelongsTo REVERSELY
		 WHERE $$.K8sResource.is_deleted == false
		 YIELD $$.K8sResource.name AS name, $$.K8sResource.kind AS kind,
		       id($$) AS uid, $$.K8sResource.name_space AS name_space`,
		vidList,
	)
	d.logger.Debug("SuggestResources GO FROM REVERSELY", zap.String("X-Trace-ID", traceID), zap.String("query", nodeQuery))
	resultSet, err := d.graphDB.ExecuteAndCheck(nodeQuery)
	if err != nil {
		d.logger.Warn("SuggestResources: GO query failed", zap.String("X-Trace-ID", traceID), zap.Error(err))
		return []map[string]interface{}{}, nil
	}

	allRows := resultSet.GetRows()
	var suggestions []map[string]interface{}
	seen := make(map[string]bool)

	for _, row := range allRows {
		name := string(row.Values[0].GetSVal())
		kind := string(row.Values[1].GetSVal())
		uid := string(row.Values[2].GetSVal())
		ns := string(row.Values[3].GetSVal())

		if !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(uid), q) {
			continue
		}
		if seen[uid] {
			continue
		}
		seen[uid] = true

		suggestions = append(suggestions, map[string]interface{}{
			"id":        uid,
			"label":     name,
			"kind":      kind,
			"namespace": ns,
		})

		if len(suggestions) >= 10 {
			break
		}
	}

	if d.resourceCache != nil {
		_ = d.resourceCache.Set(cacheKey, suggestions)
	}

	return suggestions, nil
}

// getNamespaceVID 通过 MATCH 点查找 namespace 的 VID
func (d *K8sResoureService) getNamespaceVID(namespace, traceID string) (string, error) {
	query := fmt.Sprintf(
		`MATCH (v:K8sResource{kind:"Namespace",name:%s,is_deleted:false}) RETURN id(v) AS vid`,
		strconv.Quote(namespace),
	)
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return "", err
	}
	if resultSet == nil || resultSet.GetRowSize() == 0 {
		return "", errors.New("namespace not found")
	}
	row, err := resultSet.GetRowValuesByIndex(0)
	if err != nil {
		return "", err
	}
	v, err := row.GetValueByColName("vid")
	if err != nil {
		return "", err
	}
	return v.AsString()
}

// getAllNamespaceVIDs 获取所有 Namespace 类型节点的 VID 列表
func (d *K8sResoureService) getAllNamespaceVIDs(traceID string) ([]string, error) {
	query := `MATCH (v:K8sResource{kind:"Namespace",is_deleted:false}) RETURN id(v) AS vid`
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, err
	}
	if resultSet == nil || resultSet.GetRowSize() == 0 {
		return nil, nil
	}
	var vids []string
	for i := 0; i < resultSet.GetRowSize(); i++ {
		row, err := resultSet.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		v, err := row.GetValueByColName("vid")
		if err != nil {
			continue
		}
		vid, _ := v.AsString()
		if vid != "" {
			vids = append(vids, vid)
		}
	}
	return vids, nil
}

// queryClusterScopedResources 返回集群级资源（name_space 为空串）：Node、Namespace、PersistentVolume、
// ClusterRole、CRD、CiliumIdentity 等。这类资源不属于任何命名空间、没有 BelongsTo 边，
// 无法通过 "GO FROM <nsVID> OVER BelongsTo" 遍历得到，需独立 MATCH。
// 返回列顺序与 GetAllResources 其它分支严格一致：name, kind, api_version, group, uid, name_space。
// name_space 上有 tag 索引（scripts/schema.ngql:27），空串等值查询走索引；
// 若本集群 Nebula 版本对空串 MATCH 不走索引，可改用 LOOKUP ON K8sResource WHERE name_space=="" AND is_deleted==false。
func (d *K8sResoureService) queryClusterScopedResources(traceID string) ([]*nebula.Row, error) {
	query := `MATCH (v:K8sResource{name_space:"", is_deleted:false}) RETURN v.K8sResource.name AS name, v.K8sResource.kind AS kind, v.K8sResource.api_version AS api_version, v.K8sResource.api_group AS group, id(v) AS uid, v.K8sResource.name_space AS name_space`
	d.logger.Debug("queryClusterScopedResources", zap.String("X-Trace-ID", traceID), zap.String("query", query))
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, err
	}
	return resultSet.GetRows(), nil
}

// GetAllRelationships 获取所有资源之间的关系
// 全量边查询在 Nebula 3.x 中开销极大，前端拓扑页未使用此 API，降级返回空
func (d *K8sResoureService) GetAllRelationships(_ interfaces.RequestContext) (interface{}, error) {
	// 此 API 未被前端拓扑页调用，全量边查询需要遍历 2219+ 节点 × 29 种边类型
	// 成本过高且无使用场景，返回空以保持 API 兼容
	return []map[string]interface{}{}, nil
}

// edgeRaw 遍历过程中收集的原始边数据
type edgeRaw struct {
	source string
	target string
	etype  string
}

// SearchResourceRelationship 搜索指定资源的拓扑关系（N 跳遍历）
func (d *K8sResoureService) SearchResourceRelationship(reqCtx interfaces.RequestContext) (interface{}, error) {
	traceID := reqCtx.TraceID

	resourceID := reqCtx.Query("resourceId")
	targetKind := reqCtx.QueryDefault("targetKind", "")

	if resourceID == "" {
		return nil, errors.New("resourceId is required")
	}

	return d.searchByResourceID(resourceID, reqCtx.QueryDefault("depth", "2"), targetKind, traceID)
}

// searchByResourceID 单节点模式：以指定节点为起点 N 跳双向 BFS 遍历
func (d *K8sResoureService) searchByResourceID(resourceID string, depthStr string, targetKind string, traceID string) (interface{}, error) {
	depth, err := strconv.Atoi(depthStr)
	if err != nil {
		depth = 1
		d.logger.Warn("searchByResourceID: invalid depth, using default 1", zap.String("X-Trace-ID", traceID))
	}

	const maxDepth = 10
	if depth < 1 || depth > maxDepth {
		if depth > maxDepth {
			d.logger.Warn("SearchResourceRelationship: depth exceeds maximum, capping",
				zap.Int("requested", depth), zap.Int("max", maxDepth))
		}
		depth = maxDepth
	}

	// === Step 1: GO FROM 逐节点遍历，收集节点 ID 和边 ===
	seen := map[string]bool{resourceID: true}
	currentLevel := []string{resourceID}
	resourceUIDs := []string{resourceID}
	var rawEdges []edgeRaw

	for h := 0; h < depth; h++ {
		if len(currentLevel) == 0 {
			break
		}
		var nextLevel []string

		for _, srcUID := range currentLevel {
			directions := []struct {
				suffix   string
				dirLabel string
			}{
				{"", "fwd"},
				{" REVERSELY", "rev"},
			}

			for _, dir := range directions {
				goQuery := fmt.Sprintf("GO FROM %s OVER %s%s YIELD id($$) AS target, type(edge) AS etype | LIMIT 50",
					strconv.Quote(srcUID), d.getTraversalEdgeTypes(), dir.suffix)

				resultSet, err := d.graphDB.ExecuteAndCheck(goQuery)
				if err != nil {
					d.logger.Warn("GO traversal failed",
						zap.String("X-Trace-ID", traceID),
						zap.Int("hop", h+1),
						zap.String("uid", srcUID),
						zap.String("direction", dir.dirLabel),
						zap.Error(err))
					continue
				}

				rows := resultSet.GetRows()
				for _, row := range rows {
					dstUID := string(row.Values[0].GetSVal())
					edgeType := string(row.Values[1].GetSVal())

					if dstUID == "" {
						continue
					}

					rawEdges = append(rawEdges, edgeRaw{source: srcUID, target: dstUID, etype: edgeType})

					if !seen[dstUID] {
						seen[dstUID] = true
						resourceUIDs = append(resourceUIDs, dstUID)
						nextLevel = append(nextLevel, dstUID)
					}
				}
			}
		}
		currentLevel = nextLevel
	}

	d.logger.Debug("SearchResourceRelationship traversal complete",
		zap.String("X-Trace-ID", traceID),
		zap.Int("nodes_found", len(resourceUIDs)),
		zap.Int("raw_edges", len(rawEdges)))

	// === Step 2: 分批获取节点属性 ===
	const goBatchSize = 10
	nodes := []map[string]interface{}{}
	nodeIDSet := make(map[string]bool)

	if len(resourceUIDs) > 0 {
		for i := 0; i < len(resourceUIDs); i += goBatchSize * 5 {
			end := i + goBatchSize*5
			if end > len(resourceUIDs) {
				end = len(resourceUIDs)
			}
			batch := resourceUIDs[i:end]

			uidList := listToJSON(batch)
			nodeQuery := fmt.Sprintf(
				`MATCH (v:K8sResource) WHERE id(v) IN %s AND v.K8sResource.is_deleted == false
				 RETURN v.K8sResource.uid AS uid, v.K8sResource.name AS name, v.K8sResource.kind AS kind,
				        v.K8sResource.api_version AS api_version, v.K8sResource.api_group AS group,
				        v.K8sResource.name_space AS name_space`,
				uidList,
			)

			resultSet, err := d.graphDB.ExecuteAndCheck(nodeQuery)
			if err != nil {
				d.logger.Warn("Node property query failed, skipping batch",
					zap.String("X-Trace-ID", traceID), zap.Error(err))
				continue
			}

			rows := resultSet.GetRows()
			for _, row := range rows {
				uid := string(row.Values[0].GetSVal())
				if uid == "" || nodeIDSet[uid] {
					continue
				}
				node := make(map[string]interface{})
				node["id"] = uid
				node["label"] = string(row.Values[1].GetSVal())
				node["kind"] = string(row.Values[2].GetSVal())
				node["apiVersion"] = string(row.Values[3].GetSVal())
				node["group"] = string(row.Values[4].GetSVal())
				node["namespace"] = string(row.Values[5].GetSVal())

				style := make(map[string]interface{})
				style["fill"] = getColorByKind(string(row.Values[2].GetSVal()))
				node["style"] = style

				nodes = append(nodes, node)
				nodeIDSet[uid] = true
			}
		}
	}

	// === Step 3: 从遍历数据构建边，过滤 BelongsToLabel ===
	edges := []map[string]interface{}{}
	addedEdges := make(map[string]bool)

	for _, er := range rawEdges {
		if er.etype == "BelongsToLabel" {
			continue
		}
		if !nodeIDSet[er.source] || !nodeIDSet[er.target] {
			continue
		}
		edgeKey := er.source + "|" + er.target + "|" + er.etype
		reverseKey := er.target + "|" + er.source + "|" + er.etype
		if addedEdges[edgeKey] || addedEdges[reverseKey] {
			continue
		}
		addedEdges[edgeKey] = true

		edge := make(map[string]interface{})
		edge["source"] = er.source
		edge["target"] = er.target
		edge["label"] = er.etype
		edges = append(edges, edge)
	}

	// 过滤孤立节点：只保留有边连接的节点
	if len(edges) > 0 {
		connectedUIDs := make(map[string]bool)
		for _, e := range edges {
			connectedUIDs[e["source"].(string)] = true
			connectedUIDs[e["target"].(string)] = true
		}
		filteredNodes := []map[string]interface{}{}
		for _, n := range nodes {
			if connectedUIDs[n["id"].(string)] {
				filteredNodes = append(filteredNodes, n)
			}
		}
		nodes = filteredNodes
	}

	// targetKind 过滤（后置过滤，兼容原有行为）
	if targetKind != "" {
		filteredNodes := []map[string]interface{}{}
		for _, n := range nodes {
			if strings.EqualFold(n["kind"].(string), targetKind) {
				filteredNodes = append(filteredNodes, n)
			}
		}
		nodes = filteredNodes
	}

	totalQuery := `LOOKUP ON K8sResource WHERE K8sResource.is_deleted == false YIELD id(vertex) AS vid`
	totalSet, tErr := d.graphDB.ExecuteAndCheck(totalQuery)
	totalCount := 0
	if tErr == nil && totalSet != nil {
		totalCount = totalSet.GetRowSize()
	}

	result := map[string]interface{}{
		"nodes":      nodes,
		"edges":      edges,
		"totalCount": totalCount,
	}

	return result, nil
}

// 将字符串切片转换为JSON数组格式，用于Nebula查询中的IN子句
func listToJSON(list []string) string {
	if len(list) == 0 {
		return "[]"
	}

	var result strings.Builder
	result.WriteString("[")
	for i, item := range list {
		result.WriteString(strconv.Quote(item))
		if i < len(list)-1 {
			result.WriteString(", ")
		}
	}
	result.WriteString("]")
	return result.String()
}

func (d *K8sResoureService) GetKindsAndNamespaces(_ interfaces.RequestContext) (interface{}, error) {
	const cacheKey = "metadata:kinds_namespaces"

	cached, err := d.cache.Get(cacheKey)
	if err == nil {
		var result map[string]interface{}
		if err := json.Unmarshal(cached, &result); err == nil {
			return result, nil
		}
	}

	query := `
		LOOKUP ON K8sResource WHERE K8sResource.is_deleted == false
		YIELD K8sResource.kind AS kind, K8sResource.name_space AS ns
	`
	d.logger.Debug("GetKindsAndNamespaces query", zap.String("query", query))
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("Failed to execute metadata query", zap.String("query", query), zap.Error(err))
		return nil, err
	}

	kinds := make(map[string]bool)
	namespaces := make(map[string]bool)
	rows := resultSet.GetRows()
	for _, row := range rows {
		if kind := string(row.Values[0].GetSVal()); kind != "" {
			kinds[kind] = true
		}
		if ns := string(row.Values[1].GetSVal()); ns != "" {
			namespaces[ns] = true
		}
	}

	kindsList := make([]string, 0, len(kinds))
	nsList := make([]string, 0, len(namespaces))
	for k := range kinds {
		kindsList = append(kindsList, k)
	}
	for ns := range namespaces {
		nsList = append(nsList, ns)
	}
	sort.Strings(kindsList)
	sort.Strings(nsList)

	result := map[string]interface{}{
		"kinds":      kindsList,
		"namespaces": nsList,
	}

	if data, marshalErr := json.Marshal(result); marshalErr == nil {
		_ = d.cache.Set(cacheKey, data)
	}

	return result, nil
}

func (d *K8sResoureService) invalidateMetadataCache() {
	if d.cache != nil {
		_ = d.cache.Delete("metadata:kinds_namespaces")
	}
	if d.resourceCache != nil {
		d.resourceCache.ClearAll()
	}
}

// GetResourceDefine 按需获取单个资源的完整定义
func (d *K8sResoureService) GetResourceDefine(reqCtx interfaces.RequestContext) (interface{}, error) {
	traceID := reqCtx.TraceID
	resourceID := reqCtx.Query("resourceId")
	if resourceID == "" {
		return nil, errors.New("resourceId is required")
	}
	query := fmt.Sprintf(`FETCH PROP ON K8sResource %s YIELD K8sResource.resource_define AS resource_define`, strconv.Quote(resourceID))
	d.logger.Debug("GetResourceDefine query", zap.String("X-Trace-ID", traceID), zap.String("query", query))
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("Failed to execute resource_define query", zap.String("X-Trace-ID", traceID), zap.String("query", query), zap.Error(err))
		return nil, err
	}
	rows := resultSet.GetRows()
	if len(rows) == 0 {
		return nil, errors.New("resource not found")
	}
	resourceDefine := string(rows[0].Values[0].GetSVal())
	return map[string]interface{}{"resource_define": resourceDefine}, nil
}

// 根据资源类型返回不同的颜色
func getColorByKind(kind string) string {
	colors := map[string]string{
		"Pod":                   "#1890ff",
		"Service":               "#52c41a",
		"Deployment":            "#faad14",
		"StatefulSet":           "#722ed1",
		"DaemonSet":             "#eb2f96",
		"ConfigMap":             "#a0d911",
		"Secret":                "#fa8c16",
		"PersistentVolume":      "#13c2c2",
		"PersistentVolumeClaim": "#2f54eb",
		"Namespace":             "#f5222d",
		"Node":                  "#597ef7",
		"Label":                 "#9254de", // 紫色代表标签
		"Default":               "#8c8c8c",
	}

	if color, exists := colors[kind]; exists {
		return color
	}
	return colors["Default"]
}
