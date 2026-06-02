package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gitee.com/tddh/mutong/models"
	"github.com/valyala/fastjson"
	"github.com/vesoft-inc/nebula-go/v3/nebula"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func insertK8sResourceToNebula(resource models.K8sResource) string {
	labelsJSON, _ := json.Marshal(resource.Labels)
	resourceDefineJSON, _ := json.Marshal(resource.ResourceDefine)

	escapedLabelsJSON := strconv.Quote(string(labelsJSON))
	escapedResourceDefineJSON := strconv.Quote(string(resourceDefineJSON))

	insertQuery := fmt.Sprintf(`
        INSERT VERTEX K8sResource(uid, name, api_group, labels, name_space, resource_define, kind, api_version, cluster, is_deleted, deleted_at) 
        VALUES "%s":("%s", "%s", "%s", %s, "%s", %s, "%s", "%s", "%s", %v, %d);
    `, resource.Uid, resource.Uid, resource.Name, resource.Group, escapedLabelsJSON, resource.NameSpace,
		escapedResourceDefineJSON, resource.Kind, resource.APIVersion, resource.Cluster, resource.IsDeleted, resource.DeletedAt)

	//fmt.Println(insertQuery)
	return insertQuery
}

func quoteJSONString(s string) string {
	jsonStr := `"` + s + `"`

	var p fastjson.Parser
	v, err := p.Parse(jsonStr)
	if err != nil {
		return ""
	}

	return string(v.MarshalTo(nil))
}

func (d *K8sResoureService) executenGQL(query string) ([]*nebula.Row, error) {
	d.logger.Debug("executenGQL", zap.String("nGQL", query))
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil {
		d.logger.Error("Failed to execute ngql:", zap.Error(err), zap.String("nGQL", query))
		return nil, err
	}
	if resultSet == nil {
		return nil, nil
	}
	return resultSet.GetRows(), nil
}

func (d *K8sResoureService) insertEdge(edgeType string, fromUID string, toUID string) error {
	query := fmt.Sprintf("INSERT EDGE %s () VALUES %s -> %s:();",
		edgeType,
		strconv.Quote(fromUID),
		strconv.Quote(toUID))
	d.logger.Debug("Inserting edge", zap.String("nGQL", query))
	_, err := d.graphDB.Execute(query)
	if err != nil {
		d.logger.Error("Failed to insert edge", zap.String("query", query), zap.Error(err))
		return err
	}
	return nil
	//return d.InsertEdgeWithCleanup(edgeType, fromUID, toUID, rank, 3)
}

func (d *K8sResoureService) InsertEdgeWithCleanup(edgeType string, fromUID string, toUID string, rank int64, keepVersions int) error {
	err := d.CleanupOldEdges(edgeType, fromUID, toUID, keepVersions)
	if err != nil {
		d.logger.Warn("InsertEdgeWithCleanup - Failed to cleanup old edges, proceeding with insert", zap.Error(err))
		// 即使清理失败，也继续插入新边
	}

	// 然后插入新边
	query := fmt.Sprintf("INSERT EDGE %s () VALUES %s -> %s:@%d;",
		edgeType,
		strconv.Quote(fromUID),
		strconv.Quote(toUID),
		rank)
	d.logger.Debug("InsertEdgeWithCleanup - Inserting edge", zap.String("nGQL", query))
	_, err = d.graphDB.Execute(query)
	if err != nil {
		d.logger.Error("InsertEdgeWithCleanup - Failed to insert edge", zap.String("query", query), zap.Error(err))
		return err
	}

	return nil
}

// CleanupOldEdges 清理指定边类型、源节点和目标节点的旧边，只保留最后N个版本
func (d *K8sResoureService) CleanupOldEdges(edgeType string, fromUID string, toUID string, keepVersions int) error {
	// 查询该边的所有版本并在数据库层面进行排序（升序）
	// 使用ORDER BY对rank进行升序排序，避免在应用层再次排序
	query := fmt.Sprintf(`GO FROM %s OVER %s WHERE dst(edge) == %s YIELD rank(edge) as rank | ORDER BY $-.rank ASC;`,
		strconv.Quote(fromUID),
		edgeType,
		strconv.Quote(toUID))

	d.logger.Debug("CleanupOldEdges - Querying edges", zap.String("nGQL", query))
	resultSet, err := d.graphDB.Execute(query)
	if err != nil || resultSet == nil {
		d.logger.Error("CleanupOldEdges - Failed to query edges", zap.Error(err))
		return err
	}

	var ranks []int64
	rows := resultSet.GetRows()
	for _, row := range rows {
		if err != nil {
			d.logger.Error("CleanupOldEdges - Failed to convert rank to int64", zap.Error(err), zap.String("rank_value", string(row.Values[0].GetSVal())))
			continue
		}
		ranks = append(ranks, row.Values[0].GetIVal())
	}

	if len(ranks) <= keepVersions {
		d.logger.Debug("CleanupOldEdges - No need to cleanup",
			zap.Int("current_versions", len(ranks)),
			zap.Int("keep_versions", keepVersions))
		return nil
	}

	ranksToDelete := ranks[:len(ranks)-keepVersions]

	if len(ranksToDelete) > 0 {

		var deleteEdges []string
		for _, rank := range ranksToDelete {
			deleteEdges = append(deleteEdges, fmt.Sprintf(`%s -> %s:@%d`,
				strconv.Quote(fromUID),
				strconv.Quote(toUID),
				rank))
		}

		batchDeleteQuery := fmt.Sprintf(`DELETE EDGE %s %s;`,
			edgeType,
			strings.Join(deleteEdges, ", "))

		d.logger.Debug("CleanupOldEdges - Batch deleting edges",
			zap.String("nGQL", batchDeleteQuery),
			zap.Int("edge_count", len(deleteEdges)))

		_, err := d.graphDB.Execute(batchDeleteQuery)
		if err != nil {
			d.logger.Error("CleanupOldEdges - Failed to batch delete edges",
				zap.Error(err),
				zap.String("edge_type", edgeType),
				zap.String("from_uid", fromUID),
				zap.String("to_uid", toUID),
				zap.Int("delete_count", len(deleteEdges)))
		}
	}

	return nil
}

// CleanupOutgoingEdgesByType 清理指定节点的指定类型出方向边
func (d *K8sResoureService) CleanupOutgoingEdgesByType(uid string, edgeType string) {
	outgoingEdgesQuery := fmt.Sprintf(`GO FROM %s OVER %s YIELD dst(edge) as dst_id;`,
		strconv.Quote(uid),
		edgeType)

	d.logger.Debug("CleanupOutgoingEdgesByType - Querying outgoing edges",
		zap.String("nGQL", outgoingEdgesQuery),
		zap.String("edge_type", edgeType),
		zap.String("node_uid", uid))

	resultSet, err := d.graphDB.Execute(outgoingEdgesQuery)
	if err != nil || resultSet == nil {
		d.logger.Error("CleanupOutgoingEdgesByType - Failed to query outgoing edges",
			zap.Error(err),
			zap.String("edge_type", edgeType),
			zap.String("node_uid", uid))
		return
	}

	rows := resultSet.GetRows()
	if len(rows) == 0 {
		d.logger.Debug("CleanupOutgoingEdgesByType - No outgoing edges found",
			zap.String("edge_type", edgeType),
			zap.String("node_uid", uid))
		return
	}

	// 构建批量删除语句
	var deleteEdges []string
	for _, row := range rows {
		dstUID := string(row.Values[0].GetSVal())
		deleteEdges = append(deleteEdges, fmt.Sprintf(`%s -> %s`,
			strconv.Quote(uid),
			strconv.Quote(dstUID)))
	}

	if len(deleteEdges) > 0 {
		batchDeleteQuery := fmt.Sprintf(`DELETE EDGE %s %s;`,
			edgeType,
			strings.Join(deleteEdges, ", "))

		d.logger.Debug("CleanupOutgoingEdgesByType - Batch deleting edges",
			zap.String("nGQL", batchDeleteQuery),
			zap.String("edge_type", edgeType),
			zap.String("node_uid", uid),
			zap.Int("edge_count", len(deleteEdges)))

		_, err = d.graphDB.Execute(batchDeleteQuery)
		if err != nil {
			d.logger.Error("CleanupOutgoingEdgesByType - Failed to batch delete edges",
				zap.Error(err),
				zap.String("edge_type", edgeType),
				zap.String("from_uid", uid),
				zap.Int("delete_count", len(deleteEdges)))
		}
	}
}

// CleanupAllOutgoingEdges 清理指定节点的所有出方向边
func (d *K8sResoureService) CleanupAllOutgoingEdges(uid string) {
	d.cleanupAllOutgoingEdgesWithExclusions(uid, nil)
}

// CleanupAllOutgoingEdgesExcept 清理指定节点的所有出方向边，但保留指定的边类型
func (d *K8sResoureService) CleanupAllOutgoingEdgesExcept(uid string, excludeTypes ...string) {
	d.cleanupAllOutgoingEdgesWithExclusions(uid, excludeTypes)
}

func (d *K8sResoureService) cleanupAllOutgoingEdgesWithExclusions(uid string, excludeTypes []string) {
	edgesQuery := fmt.Sprintf(`GO FROM %s OVER * YIELD dst(edge) as dst_id, type(edge) as edge_type`,
		strconv.Quote(uid))

	d.logger.Debug("CleanupAllOutgoingEdges - Querying all outgoing edges",
		zap.String("node_uid", uid))

	resultSet, err := d.graphDB.Execute(edgesQuery)
	if err != nil || resultSet == nil {
		d.logger.Error("CleanupAllOutgoingEdges - Failed to query outgoing edges",
			zap.Error(err),
			zap.String("node_uid", uid))
		return
	}

	rows := resultSet.GetRows()
	if len(rows) == 0 {
		d.logger.Debug("CleanupAllOutgoingEdges - No outgoing edges found",
			zap.String("node_uid", uid))
		return
	}

	excludeSet := make(map[string]bool, len(excludeTypes))
	for _, et := range excludeTypes {
		excludeSet[et] = true
	}

	edgesByType := make(map[string][]string)
	for _, row := range rows {
		edgeType := string(row.Values[1].GetSVal())
		dstUID := string(row.Values[0].GetSVal())
		if excludeSet[edgeType] {
			continue
		}
		edgesByType[edgeType] = append(edgesByType[edgeType], dstUID)
	}

	const batchSize = 500
	totalDeleted := 0
	for edgeType, dstUIDs := range edgesByType {
		for i := 0; i < len(dstUIDs); i += batchSize {
			end := i + batchSize
			if end > len(dstUIDs) {
				end = len(dstUIDs)
			}
			batch := dstUIDs[i:end]

			var deleteEdges []string
			for _, dstUID := range batch {
				deleteEdges = append(deleteEdges, fmt.Sprintf("%s -> %s",
					strconv.Quote(uid),
					strconv.Quote(dstUID)))
			}

			batchDeleteQuery := fmt.Sprintf(`DELETE EDGE %s %s;`,
				edgeType,
				strings.Join(deleteEdges, ", "))

			d.logger.Debug("CleanupAllOutgoingEdges - Batch deleting edges",
				zap.String("edge_type", edgeType),
				zap.String("node_uid", uid),
				zap.Int("batch_start", i),
				zap.Int("batch_size", len(deleteEdges)))

			_, err = d.graphDB.Execute(batchDeleteQuery)
			if err != nil {
				d.logger.Error("CleanupAllOutgoingEdges - Failed to batch delete edges",
					zap.Error(err),
					zap.String("edge_type", edgeType),
					zap.String("node_uid", uid))
			} else {
				totalDeleted += len(deleteEdges)
			}
		}
	}
	d.logger.Debug("CleanupAllOutgoingEdges - Completed",
		zap.String("node_uid", uid),
		zap.Int("total_deleted", totalDeleted))
}

// ForceSyncResources compares Nebula with Live K8s API and cleans up deleted resources

// ForceSyncResources compares live K8s state with Nebula and marks deleted resources
func (d *K8sResoureService) ForceSyncResources() {
	d.logger.Info("Force sync started")

	// 1. Get all UIDs from Nebula with is_deleted: false
	query := `LOOKUP ON K8sResource WHERE K8sResource.is_deleted == false YIELD K8sResource.uid AS uid`
	resultSet, err := d.graphDB.Execute(query)
	if err != nil || resultSet == nil {
		d.logger.Error("Failed to query Nebula for sync", zap.Error(err))
		return
	}

	nebulaUIDs := make(map[string]bool)
	if resultSet.GetRowSize() > 0 {
		for i := 0; i < resultSet.GetRowSize(); i++ {
			row, err := resultSet.GetRowValuesByIndex(i)
			if err != nil {
				continue
			}
			val, _ := row.GetValueByColName("uid")
			if val != nil {
				if uid, e := val.AsString(); e == nil {
					nebulaUIDs[uid] = true
				}
			}
		}
	}

	d.logger.Info("Force sync: Nebula scan done", zap.Int("count", len(nebulaUIDs)))

	// 2. Get all UIDs from K8s API (all resource types via dynamic client)
	k8sUIDs := make(map[string]bool)
	totalGVRs := 0

	for _, ctxData := range d.cluster.K8sClusterClient {
		if ctxData.RootRestConfig == nil {
			continue
		}

		dynamicClient, err := dynamic.NewForConfig(ctxData.RootRestConfig)
		if err != nil {
			d.logger.Error("Failed to create dynamic client for cluster", zap.Error(err))
			continue
		}

		d.apiResourcesMu.RLock()
		gvrCount := len(d.apiResources)
		d.logger.Info("Force sync: apiResources snapshot", zap.Int("gvrCount", gvrCount))
		for gvrStr := range d.apiResources {
			// Format: "group/version, Resource=resource" (k8s.io/apimachinery v0.34.1)
			slashIdx := strings.IndexByte(gvrStr, '/')
			resourcePrefix := ", Resource="
			resIdx := strings.Index(gvrStr, resourcePrefix)
			if slashIdx == -1 || resIdx == -1 {
				d.logger.Warn("Force sync: skip malformed GVR", zap.String("gvr", gvrStr))
				continue
			}
			gvr := schema.GroupVersionResource{
				Group:    gvrStr[:slashIdx],
				Version:  gvrStr[slashIdx+1 : resIdx],
				Resource: gvrStr[resIdx+len(resourcePrefix):],
			}
			d.logger.Info("Force sync: listing GVR", zap.String("gvr", gvrStr))
			list, err := dynamicClient.Resource(gvr).Namespace("").List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				d.logger.Warn("Force sync: skip GVR", zap.String("gvr", gvrStr), zap.Error(err))
				continue
			}
			totalGVRs++
			for _, item := range list.Items {
				k8sUIDs[string(item.GetUID())] = true
			}
			d.logger.Info("Force sync: GVR listed", zap.String("gvr", gvrStr), zap.Int("items", len(list.Items)))
		}
		d.apiResourcesMu.RUnlock()
	}

	d.logger.Info("Force sync: K8s scan done", zap.Int("gvrs", totalGVRs), zap.Int("total_uids", len(k8sUIDs)))

	if totalGVRs == 0 {
		d.logger.Warn("Force sync aborted: no GVRs registered yet, informer may not be ready")
		return
	}

	// 3. Compare and mark as deleted in Nebula
	deletedCount := 0
	for uid := range nebulaUIDs {
		if !k8sUIDs[uid] {
			d.logger.Debug("Marking resource as deleted", zap.String("uid", uid))
			cleanupQuery := fmt.Sprintf(`UPDATE VERTEX ON K8sResource "%s" SET is_deleted = true`, uid)
			_, _ = d.graphDB.Execute(cleanupQuery)
			d.CleanupAllOutgoingEdges(uid)
			deletedCount++
		}
	}

	d.logger.Info("Force sync completed", zap.Int("deleted", deletedCount))
}

func (d *K8sResoureService) isAlreadyDeleted(uid string) bool {
	query := fmt.Sprintf(`FETCH PROP ON K8sResource %s YIELD K8sResource.is_deleted`, strconv.Quote(uid))
	resultSet, err := d.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		return false
	}
	row, _ := resultSet.GetRowValuesByIndex(0)
	val, _ := row.GetValueByColName("is_deleted")
	if val == nil {
		return false
	}
	deleted, err := val.AsBool()
	if err != nil {
		return false
	}
	return deleted
}

func (d *K8sResoureService) StartPeriodicCleanup(interval time.Duration, retentionDays int, batchSize int) {
	d.logger.Info("Starting periodic cleanup of deleted K8s resources",
		zap.Duration("interval", interval),
		zap.Int("retentionDays", retentionDays),
		zap.Int("batchSize", batchSize))

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-d.stopCh:
				d.logger.Info("Periodic cleanup stopped")
				return
			case <-ticker.C:
				d.cleanupExpiredDeletedVertices(retentionDays, batchSize)
			}
		}
	}()
}

func (d *K8sResoureService) cleanupExpiredDeletedVertices(retentionDays int, batchSize int) {
	d.logger.Debug("Starting cleanup of expired deleted vertices",
		zap.Int("retentionDays", retentionDays))

	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	query := fmt.Sprintf(`
		MATCH (v:K8sResource) 
		WHERE v.is_deleted == true AND v.K8sResource.deleted_at < %d 
		RETURN v.K8sResource.uid as uid 
		LIMIT %d`, cutoff, batchSize)

	resultSet, err := d.graphDB.Execute(query)
	if err != nil || resultSet == nil {
		d.logger.Error("Cleanup query failed", zap.Error(err))
		return
	}

	rows := resultSet.GetRows()
	if len(rows) == 0 {
		d.logger.Debug("No expired deleted vertices found")
		return
	}

	deletedCount := 0
	for _, row := range rows {
		uid := string(row.Values[0].GetSVal())
		delQuery := fmt.Sprintf(`DELETE VERTEX "%s";`, uid)
		_, err := d.graphDB.Execute(delQuery)
		if err != nil {
			d.logger.Error("Failed to delete vertex", zap.String("uid", uid), zap.Error(err))
			continue
		}
		deletedCount++
	}

	d.logger.Debug("Cleanup completed",
		zap.Int("deletedCount", deletedCount),
		zap.Int("totalFound", len(rows)))
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
