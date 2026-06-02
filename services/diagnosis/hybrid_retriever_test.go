package diagnosis

import (
	"testing"

	"gitee.com/tddh/mutong/models/diagnosis"
)

func TestFuseAndRank_MergeAndDedup(t *testing.T) {
	h := &HybridRetriever{}

	vector := []diagnosis.FaultReportVector{
		{AlertFingerprint: "fp-A", Summary: "OOM on order-service"},
		{AlertFingerprint: "fp-B", Summary: "CPU throttle on payment"},
	}
	graph := []diagnosis.GraphCaseResult{
		{Fingerprint: "fp-A", MatchReason: "SameOwner", TopologicalScore: 0.8},
		{Fingerprint: "fp-C", MatchReason: "SameNode", TopologicalScore: 0.5},
	}

	results := h.fuseAndRank(vector, graph, 0.6, 10)

	if len(results) != 3 {
		t.Fatalf("expected 3 deduped results, got %d", len(results))
	}
	if results[0].Fingerprint != "fp-A" {
		t.Errorf("expected fp-A first (双命中), got %s", results[0].Fingerprint)
	}
	if len(results[0].MatchReasons) == 0 || results[0].MatchReasons[0] != "SameOwner" {
		t.Errorf("fp-A should have MatchReasons containing SameOwner, got %v", results[0].MatchReasons)
	}
}

func TestFuseAndRank_EmptyInputs(t *testing.T) {
	h := &HybridRetriever{}

	results := h.fuseAndRank(nil, nil, 0.6, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty inputs, got %d", len(results))
	}
}

func TestFuseAndRank_Limit(t *testing.T) {
	h := &HybridRetriever{}

	vector := []diagnosis.FaultReportVector{
		{AlertFingerprint: "fp-001", Summary: "A"},
		{AlertFingerprint: "fp-002", Summary: "B"},
		{AlertFingerprint: "fp-003", Summary: "C"},
	}

	results := h.fuseAndRank(vector, nil, 0.6, 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results (limit), got %d", len(results))
	}
}

func TestFuseAndRank_TopoOnly(t *testing.T) {
	h := &HybridRetriever{}

	graph := []diagnosis.GraphCaseResult{
		{Fingerprint: "fp-X", MatchReason: "SameNode", TopologicalScore: 0.5},
	}

	results := h.fuseAndRank(nil, graph, 0.6, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 topo-only result, got %d", len(results))
	}
	if results[0].Fingerprint != "fp-X" {
		t.Errorf("expected fp-X, got %s", results[0].Fingerprint)
	}
	if results[0].SemanticScore != 0 {
		t.Errorf("expected semantic score 0 for topo-only, got %.2f", results[0].SemanticScore)
	}
}
