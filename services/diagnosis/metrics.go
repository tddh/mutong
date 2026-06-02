package diagnosis

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	diagnosisTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "diagnosis_total",
			Help: "Total number of diagnoses performed",
		},
		[]string{"path"},
	)

	diagnosisDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "diagnosis_duration_seconds",
			Help:    "Time spent performing a diagnosis",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)

	llmFallbackTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llm_fallback_total",
			Help: "Total number of LLM fallback attempts",
		},
		[]string{"result"},
	)

	diagnosisConfidence = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "diagnosis_confidence",
			Help:    "Confidence scores of diagnoses",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		},
	)

	topologyNodesCount = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "topology_nodes_count",
			Help:    "Number of nodes in topology snapshots",
			Buckets: []float64{1, 5, 10, 20, 50, 100},
		},
	)

	topologyEdgesCount = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "topology_edges_count",
			Help:    "Number of edges in topology snapshots",
			Buckets: []float64{0, 5, 10, 20, 50, 100},
		},
	)
)

func init() {
	prometheus.MustRegister(diagnosisTotal)
	prometheus.MustRegister(diagnosisDuration)
	prometheus.MustRegister(llmFallbackTotal)
	prometheus.MustRegister(diagnosisConfidence)
	prometheus.MustRegister(topologyNodesCount)
	prometheus.MustRegister(topologyEdgesCount)
}

type DiagnosisMetrics struct{}

func (m *DiagnosisMetrics) RecordDiagnosis(path string, duration time.Duration, confidence float64) {
	diagnosisTotal.WithLabelValues(path).Inc()
	diagnosisDuration.WithLabelValues(path).Observe(duration.Seconds())
	diagnosisConfidence.Observe(confidence)
}

func (m *DiagnosisMetrics) RecordLLMFallback(success bool) {
	result := "success"
	if !success {
		result = "failed"
	}
	llmFallbackTotal.WithLabelValues(result).Inc()
}

func (m *DiagnosisMetrics) RecordTopologySnapshot(nodes, edges int) {
	topologyNodesCount.Observe(float64(nodes))
	topologyEdgesCount.Observe(float64(edges))
}
