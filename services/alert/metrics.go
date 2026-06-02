package alert

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	alertsReceivedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alerts_received_total",
			Help: "Total number of alerts received",
		},
		[]string{"status"},
	)

	alertsSuppressedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alerts_suppressed_total",
			Help: "Total number of alerts suppressed",
		},
		[]string{"reason"},
	)

	alertsNotifiedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alerts_notified_total",
			Help: "Total number of alerts notified",
		},
		[]string{"channel"},
	)

	alertsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "alerts_active",
			Help: "Current number of active alerts",
		},
		[]string{"status"},
	)

	alertProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "alert_processing_duration_seconds",
			Help:    "Time spent processing an alert",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"suppressed"},
	)

	TopologyCacheHits   = prometheus.NewCounter(prometheus.CounterOpts{Name: "topology_cache_hits_total", Help: "Total topology cache hits"})
	TopologyCacheMisses = prometheus.NewCounter(prometheus.CounterOpts{Name: "topology_cache_misses_total", Help: "Total topology cache misses"})

	alertEnrichDegradedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "alert_enrichment_degraded_total",
		Help: "Total alerts enriched with fallback due to NebulaGraph timeout/error",
	})

	alertProcessingLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "alert_batch_processing_latency_seconds",
		Help:    "Total time to process a batch of alerts",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
	})
)

func init() {
	prometheus.MustRegister(alertsReceivedTotal)
	prometheus.MustRegister(alertsSuppressedTotal)
	prometheus.MustRegister(alertsNotifiedTotal)
	prometheus.MustRegister(alertsActive)
	prometheus.MustRegister(alertProcessingDuration)
	prometheus.MustRegister(TopologyCacheHits)
	prometheus.MustRegister(TopologyCacheMisses)
	prometheus.MustRegister(alertEnrichDegradedTotal)
	prometheus.MustRegister(alertProcessingLatency)
}

type AlertMetrics struct {
	receivedTotal   atomic.Int64
	suppressedTotal atomic.Int64
	notifiedTotal   atomic.Int64
	activeFiring    atomic.Int64
	activeResolved  atomic.Int64
}

func (m *AlertMetrics) GetReceivedTotal() int64   { return m.receivedTotal.Load() }
func (m *AlertMetrics) GetSuppressedTotal() int64 { return m.suppressedTotal.Load() }
func (m *AlertMetrics) GetNotifiedTotal() int64   { return m.notifiedTotal.Load() }
func (m *AlertMetrics) GetActiveFiring() int64    { return m.activeFiring.Load() }
func (m *AlertMetrics) GetActiveResolved() int64  { return m.activeResolved.Load() }

func (m *AlertMetrics) RecordReceived(status string) {
	m.receivedTotal.Add(1)
	alertsReceivedTotal.WithLabelValues(status).Inc()
}

func (m *AlertMetrics) RecordSuppressed(reason string) {
	m.suppressedTotal.Add(1)
	alertsSuppressedTotal.WithLabelValues(reason).Inc()
}

func (m *AlertMetrics) RecordNotified(channel string) {
	m.notifiedTotal.Add(1)
	alertsNotifiedTotal.WithLabelValues(channel).Inc()
}

func (m *AlertMetrics) SetActiveFiring(count int64) {
	m.activeFiring.Store(count)
	alertsActive.WithLabelValues("firing").Set(float64(count))
}

func (m *AlertMetrics) SetActiveResolved(count int64) {
	m.activeResolved.Store(count)
	alertsActive.WithLabelValues("resolved").Set(float64(count))
}

func (m *AlertMetrics) RecordProcessingDuration(start time.Time, suppressed bool) {
	duration := time.Since(start).Seconds()
	alertProcessingDuration.WithLabelValues(fmt.Sprintf("%t", suppressed)).Observe(duration)
}

func (m *AlertMetrics) RecordEnrichmentDegraded(fingerprint string) {
	alertEnrichDegradedTotal.Inc()
}

func (m *AlertMetrics) RecordProcessingLatency(d time.Duration) {
	alertProcessingLatency.Observe(d.Seconds())
}
