package diagnosis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type HybridRetriever struct {
	logger          interfaces.Logger
	vectorRetriever *VectorRetriever
	topoQuerier     *TopologyQuerier
	db              *gorm.DB
}

func NewHybridRetriever(
	logger interfaces.Logger,
	vectorRetriever *VectorRetriever,
	topoQuerier *TopologyQuerier,
	db *gorm.DB,
) *HybridRetriever {
	return &HybridRetriever{
		logger:          logger,
		vectorRetriever: vectorRetriever,
		topoQuerier:     topoQuerier,
		db:              db,
	}
}

func (h *HybridRetriever) Search(
	ctx context.Context,
	req diagnosis.HybridSearchRequest,
) ([]diagnosis.HybridSearchResult, error) {
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.SemanticWeight <= 0 {
		req.SemanticWeight = 0.6
	}

	var (
		vectorResults []diagnosis.FaultReportVector
		graphResults  []diagnosis.GraphCaseResult
		wg            sync.WaitGroup
	)

	if req.AlertFingerprint != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := h.vectorRetriever.SearchSimilarByFingerprint(ctx, req.AlertFingerprint, req.Limit*2)
			if err != nil {
				h.logger.Warn("HybridRetriever: vector search failed",
					zap.String("fingerprint", req.AlertFingerprint), zap.Error(err))
				return
			}
			vectorResults = results
		}()
	}

	if req.ResourceUID != "" && req.ResourceKind == "Pod" && req.Namespace != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			graphResults = append(graphResults, h.searchBySameOwner(req.ResourceUID, req.Namespace)...)
			graphResults = append(graphResults, h.searchBySameNode(req.ResourceUID)...)
		}()
	}

	wg.Wait()

	return h.fuseAndRank(vectorResults, graphResults, req.SemanticWeight, req.Limit), nil
}

func (h *HybridRetriever) searchBySameOwner(uid, namespace string) []diagnosis.GraphCaseResult {
	ownerQ := fmt.Sprintf(
		`MATCH (pod:K8sResource)-[:OwnedBy]->(owner:K8sResource)
		 WHERE id(pod) == %s
		 RETURN owner.K8sResource.name AS owner_name, owner.K8sResource.kind AS owner_kind
		 LIMIT 1`,
		strconv.Quote(uid),
	)
	rs, err := h.topoQuerier.graphDB.ExecuteAndCheck(ownerQ)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return nil
	}
	row, _ := rs.GetRowValuesByIndex(0)
	ownerNameVal, _ := row.GetValueByColName("owner_name")
	ownerKindVal, _ := row.GetValueByColName("owner_kind")
	ownerName, _ := ownerNameVal.AsString()
	ownerKind, _ := ownerKindVal.AsString()
	if ownerName == "" {
		return nil
	}

	siblingQ := fmt.Sprintf(
		`MATCH (owner:K8sResource{kind:%s, name:%s})<-[:OwnedBy]-(pod:K8sResource{kind:"Pod", name_space:%s})
		 WHERE id(pod) != %s
		 RETURN pod.K8sResource.name AS pod_name
		 LIMIT 10`,
		strconv.Quote(ownerKind), strconv.Quote(ownerName),
		strconv.Quote(namespace), strconv.Quote(uid),
	)
	srs, err := h.topoQuerier.graphDB.ExecuteAndCheck(siblingQ)
	if err != nil || srs == nil {
		return nil
	}

	var podNames []string
	for i := 0; i < srs.GetRowSize(); i++ {
		r, _ := srs.GetRowValuesByIndex(i)
		nv, _ := r.GetValueByColName("pod_name")
		name, _ := nv.AsString()
		if name != "" {
			podNames = append(podNames, name)
		}
	}
	if len(podNames) == 0 {
		return nil
	}

	return h.lookupFaultVectorsByResourceNames(podNames, "Pod", "SameOwner", 0.8)
}

func (h *HybridRetriever) searchBySameNode(uid string) []diagnosis.GraphCaseResult {
	nodeQ := fmt.Sprintf(
		`MATCH (pod:K8sResource)-[:RunsOn]->(node:K8sResource)
		 WHERE id(pod) == %s
		 RETURN id(node) AS node_uid
		 LIMIT 1`,
		strconv.Quote(uid),
	)
	rs, err := h.topoQuerier.graphDB.ExecuteAndCheck(nodeQ)
	if err != nil || rs == nil || rs.GetRowSize() == 0 {
		return nil
	}
	row, _ := rs.GetRowValuesByIndex(0)
	nodeUIDVal, _ := row.GetValueByColName("node_uid")
	nodeUID, _ := nodeUIDVal.AsString()
	if nodeUID == "" {
		return nil
	}

	siblingQ := fmt.Sprintf(
		`MATCH (pod:K8sResource{kind:"Pod"})-[:RunsOn]->(node:K8sResource)
		 WHERE id(node) == %s AND id(pod) != %s
		 RETURN pod.K8sResource.name AS pod_name
		 LIMIT 10`,
		strconv.Quote(nodeUID), strconv.Quote(uid),
	)
	srs, _ := h.topoQuerier.graphDB.ExecuteAndCheck(siblingQ)

	var podNames []string
	if srs != nil {
		for i := 0; i < srs.GetRowSize(); i++ {
			r, _ := srs.GetRowValuesByIndex(i)
			nv, _ := r.GetValueByColName("pod_name")
			name, _ := nv.AsString()
			if name != "" {
				podNames = append(podNames, name)
			}
		}
	}
	if len(podNames) == 0 {
		return nil
	}

	return h.lookupFaultVectorsByResourceNames(podNames, "Pod", "SameNode", 0.5)
}

func (h *HybridRetriever) lookupFaultVectorsByResourceNames(
	names []string,
	resourceKind string,
	matchReason string,
	topoScore float64,
) []diagnosis.GraphCaseResult {
	var records []diagnosis.FaultReportVector
	if err := h.db.
		Where("resource_kind = ? AND resource_name IN ?", resourceKind, names).
		Order("created_at DESC").
		Limit(5).
		Find(&records).Error; err != nil {
		h.logger.Warn("HybridRetriever: lookup fault_report_vectors failed",
			zap.String("match_reason", matchReason), zap.Error(err))
		return nil
	}

	var results []diagnosis.GraphCaseResult
	for _, r := range records {
		results = append(results, diagnosis.GraphCaseResult{
			Fingerprint:      r.AlertFingerprint,
			Summary:          r.Summary,
			MatchReason:      matchReason,
			RelationPath:     resourceKind,
			TopologicalScore: topoScore,
		})
	}
	return results
}

func (h *HybridRetriever) fuseAndRank(
	vectorResults []diagnosis.FaultReportVector,
	graphResults []diagnosis.GraphCaseResult,
	semanticWeight float64,
	limit int,
) []diagnosis.HybridSearchResult {
	topoWeight := 1.0 - semanticWeight
	merged := make(map[string]*diagnosis.HybridSearchResult)

	for i, vr := range vectorResults {
		semScore := 1.0 - float64(i)*0.05
		if semScore < 0.3 {
			semScore = 0.3
		}
		merged[vr.AlertFingerprint] = &diagnosis.HybridSearchResult{
			Fingerprint:   vr.AlertFingerprint,
			Summary:       vr.Summary,
			SemanticScore: semScore,
			CombinedScore: semScore * semanticWeight,
		}
	}

	for _, gr := range graphResults {
		if existing, ok := merged[gr.Fingerprint]; ok {
			existing.TopologicalScore += gr.TopologicalScore
			if existing.TopologicalScore > 1.0 {
				existing.TopologicalScore = 1.0
			}
			existing.MatchReasons = append(existing.MatchReasons, gr.MatchReason)
			existing.CombinedScore = existing.SemanticScore*semanticWeight + existing.TopologicalScore*topoWeight
		} else {
			merged[gr.Fingerprint] = &diagnosis.HybridSearchResult{
				Fingerprint:      gr.Fingerprint,
				Summary:          gr.Summary,
				TopologicalScore: gr.TopologicalScore,
				CombinedScore:    gr.TopologicalScore * topoWeight,
				MatchReasons:     []string{gr.MatchReason},
			}
		}
	}

	results := make([]diagnosis.HybridSearchResult, 0, len(merged))
	for _, r := range merged {
		results = append(results, *r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CombinedScore > results[j].CombinedScore
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}
