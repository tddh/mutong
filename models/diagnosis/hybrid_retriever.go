package diagnosis

type GraphCaseResult struct {
	Fingerprint      string  `json:"fingerprint"`
	Summary          string  `json:"summary"`
	MatchReason      string  `json:"match_reason"`
	RelationPath     string  `json:"relation_path"`
	TopologicalScore float64 `json:"topological_score"`
}

type HybridSearchResult struct {
	Fingerprint      string   `json:"fingerprint"`
	Summary          string   `json:"summary"`
	SemanticScore    float64  `json:"semantic_score"`
	TopologicalScore float64  `json:"topological_score"`
	CombinedScore    float64  `json:"combined_score"`
	MatchReasons     []string `json:"match_reasons"`
}

type HybridSearchRequest struct {
	AlertFingerprint string  `json:"alert_fingerprint"`
	ResourceUID      string  `json:"resource_uid"`
	ResourceKind     string  `json:"resource_kind"`
	ResourceName     string  `json:"resource_name"`
	Namespace        string  `json:"namespace"`
	QueryText        string  `json:"query_text"`
	Limit            int     `json:"limit"`
	SemanticWeight   float64 `json:"-"`
}
