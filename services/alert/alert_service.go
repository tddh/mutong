package alert

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/tddh/mutong/interfaces"
	alert_interfaces "gitee.com/tddh/mutong/interfaces/alert"
	alert_models "gitee.com/tddh/mutong/models/alert"
	retrospective_model "gitee.com/tddh/mutong/models/retrospective"
)

// AutoDiagnosisPipelineInterface defines the minimal API the auto-diagnosis pipeline
// must provide for integration with the AlertService without introducing a hard
// dependency on a concrete implementation.
type AutoDiagnosisPipelineInterface interface {
	OnAlertProcessed(ctx context.Context, alert *alert_models.ProcessedAlert)
	SetExecutor(executor interfaces.Executor)
}

// DiagnosisCacheInvalidator defines the minimal API for invalidating diagnosis cache.
type DiagnosisCacheInvalidator interface {
	InvalidateDiagnosis(ctx context.Context, fingerprint string) error
}

type AlertService struct {
	logger     interfaces.Logger
	graphDB    interfaces.GraphDB
	enricher   alert_interfaces.AlertEnricher
	suppressor alert_interfaces.AlertSuppressor
	router     alert_interfaces.AlertRouter
	notifier   alert_interfaces.AlertNotifier
	storage    alert_interfaces.AlertStorage
	metrics    *AlertMetrics
	aggregator *AlertAggregator

	cleanupInterval   time.Duration
	retentionDuration time.Duration
	stopOnce          sync.Once
	stopCh            chan struct{}

	// auto-diagnosis pipeline hook (optional)
	autoDiagnosisPipeline AutoDiagnosisPipelineInterface

	diagnosisCacheInvalidator DiagnosisCacheInvalidator

	db *gorm.DB

	alertSources   map[string]*alert_models.AlertSource
	bizCtxProvider interfaces.BusinessContextProvider
}

func (s *AlertService) SetAlertSources(sources map[string]*alert_models.AlertSource) {
	s.alertSources = sources
}

func (s *AlertService) SetBizCtxProvider(p interfaces.BusinessContextProvider) {
	s.bizCtxProvider = p
	if e, ok := s.enricher.(*AlertEnricher); ok {
		e.WithBusinessContextProvider(p)
	}
}

// AlertServiceMetrics 快照指标
type AlertServiceMetrics struct {
	ReceivedTotal   int64 `json:"receivedTotal"`
	SuppressedTotal int64 `json:"suppressedTotal"`
	NotifiedTotal   int64 `json:"notifiedTotal"`
	ActiveFiring    int64 `json:"activeFiring"`
	ActiveResolved  int64 `json:"activeResolved"`
}

// AlertServiceStatus 表示告警服务状态信息
type AlertServiceStatus struct {
	Metrics           AlertServiceMetrics `json:"metrics"`
	SuppressorEnabled bool                `json:"suppressorEnabled"`
}

func (s *AlertService) GetStatus() AlertServiceStatus {
	return AlertServiceStatus{
		Metrics: AlertServiceMetrics{
			ReceivedTotal:   s.metrics.GetReceivedTotal(),
			SuppressedTotal: s.metrics.GetSuppressedTotal(),
			NotifiedTotal:   s.metrics.GetNotifiedTotal(),
			ActiveFiring:    s.metrics.GetActiveFiring(),
			ActiveResolved:  s.metrics.GetActiveResolved(),
		},
		SuppressorEnabled: s.suppressor != nil,
	}
}

func (s *AlertService) GetSuppressionStatus() SuppressorStatus {
	if suppressor, ok := s.suppressor.(*AlertSuppressor); ok {
		return suppressor.GetStatus()
	}
	return SuppressorStatus{}
}

func NewAlertService(
	logger interfaces.Logger,
	graphDB interfaces.GraphDB,
	enricher alert_interfaces.AlertEnricher,
	suppressor alert_interfaces.AlertSuppressor,
	router alert_interfaces.AlertRouter,
	notifier alert_interfaces.AlertNotifier,
	storage alert_interfaces.AlertStorage,
	aggregator *AlertAggregator,
) *AlertService {
	return &AlertService{
		logger:            logger,
		graphDB:           graphDB,
		enricher:          enricher,
		suppressor:        suppressor,
		router:            router,
		notifier:          notifier,
		storage:           storage,
		aggregator:        aggregator,
		metrics:           &AlertMetrics{},
		cleanupInterval:   1 * time.Hour,
		retentionDuration: 24 * time.Hour,
		stopCh:            make(chan struct{}),
	}
}

func (s *AlertService) WithCleanupConfig(interval, retention time.Duration) *AlertService {
	if interval > 0 {
		s.cleanupInterval = interval
	}
	if retention > 0 {
		s.retentionDuration = retention
	}
	return s
}

// SetAutoDiagnosisPipeline attaches an auto-diagnosis pipeline implementation to this service
func (s *AlertService) SetAutoDiagnosisPipeline(p AutoDiagnosisPipelineInterface) {
	s.autoDiagnosisPipeline = p
}

func (s *AlertService) WithDiagnosisCacheInvalidator(inv DiagnosisCacheInvalidator) {
	s.diagnosisCacheInvalidator = inv
}

func (s *AlertService) WithDB(db *gorm.DB) *AlertService {
	s.db = db
	return s
}

func (s *AlertService) loadStatsFromDB() {
	if s.db == nil {
		return
	}

	var stats []alert_models.StatsModel
	if err := s.db.Find(&stats).Error; err != nil {
		s.logger.Warn("Failed to load alert stats from DB", zap.Error(err))
		return
	}

	for _, st := range stats {
		switch st.StatKey {
		case "received_total":
			s.metrics.receivedTotal.Add(st.StatValue)
		case "suppressed_total":
			s.metrics.suppressedTotal.Add(st.StatValue)
		case "notified_total":
			s.metrics.notifiedTotal.Add(st.StatValue)
		}
	}
}

func (s *AlertService) syncStatsToDB() {
	if s.db == nil {
		return
	}

	stats := []alert_models.StatsModel{
		{StatKey: "received_total", StatValue: s.metrics.receivedTotal.Load()},
		{StatKey: "suppressed_total", StatValue: s.metrics.suppressedTotal.Load()},
		{StatKey: "notified_total", StatValue: s.metrics.notifiedTotal.Load()},
	}

	for _, st := range stats {
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "stat_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"stat_value", "updated_at"}),
		}).Create(&st).Error; err != nil {
			s.logger.Warn("Failed to sync alert stats to DB", zap.String("key", st.StatKey), zap.Error(err))
		}
	}
}

func (s *AlertService) StartStatsSync(interval time.Duration) {
	s.loadStatsFromDB()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				s.logger.Info("Alert stats sync stopped")
				return
			case <-ticker.C:
				s.syncStatsToDB()
			}
		}
	}()
}

func (s *AlertService) StartCleanupScheduler() {
	ticker := time.NewTicker(s.cleanupInterval)
	go func() {
		defer ticker.Stop()
		s.logger.Info("Alert cleanup scheduler started",
			zap.Duration("cleanup_interval", s.cleanupInterval),
			zap.Duration("retention_duration", s.retentionDuration))
		for {
			select {
			case <-ticker.C:
				s.performCleanup()
			case <-s.stopCh:
				s.logger.Info("Alert cleanup scheduler stopped")
				return
			}
		}
	}()
}

func (s *AlertService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// StartAggregator starts the alert aggregation scheduler if an aggregator is configured.
func (s *AlertService) StartAggregator() {
	if s.aggregator == nil {
		return
	}
	s.aggregator.StartFlushScheduler(s.aggregator.window, func(groups []*AlertGroup) {
		s.notifyAggregated(groups)
	})
}

// StopAggregator stops the aggregation scheduler if it is running.
func (s *AlertService) StopAggregator() {
	if s.aggregator != nil {
		s.aggregator.Stop()
	}
}

// notifyAggregated forwards aggregated groups to the notifier implementation if available.
func (s *AlertService) notifyAggregated(groups []*AlertGroup) {
	if len(groups) == 0 {
		return
	}
	// Try to call the concrete notifier's aggregated handler if available
	if na, ok := s.notifier.(*AlertNotifier); ok {
		_ = na.sendAggregatedAlerts(groups)
	}
}

func (s *AlertService) performCleanup() {
	count := s.storage.CleanupResolvedAlerts(s.retentionDuration)
	stats := s.storage.GetStats()
	s.metrics.SetActiveFiring(stats["firing"])
	s.metrics.SetActiveResolved(stats["resolved"])
	s.logger.Info("Alert cleanup completed",
		zap.Int("cleaned_count", count),
		zap.Int64("total_alerts", stats["total"]),
		zap.Int64("firing_alerts", stats["firing"]),
		zap.Int64("resolved_alerts", stats["resolved"]))
}

func (s *AlertService) GetMemoryStats() map[string]interface{} {
	storageStats := s.storage.GetStats()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]interface{}{
		"alerts":         storageStats,
		"heap_alloc_mb":  m.HeapAlloc / 1024 / 1024,
		"heap_sys_mb":    m.HeapSys / 1024 / 1024,
		"total_alloc_mb": m.TotalAlloc / 1024 / 1024,
		"sys_mb":         m.Sys / 1024 / 1024,
		"num_gc":         m.NumGC,
	}
}

type singleAlertResult struct {
	index int
	alert *alert_models.ProcessedAlert
}

const alertWorkerCount = 20

func (s *AlertService) Process(ctx context.Context, payload *alert_models.WebhookPayload) ([]*alert_models.ProcessedAlert, error) {
	s.logger.Info("Processing alert webhook",
		zap.String("groupKey", payload.GroupKey),
		zap.String("status", payload.Status),
		zap.Int("alertCount", len(payload.Alerts)))

	s.metrics.RecordReceived(payload.Status)
	batchStart := time.Now()

	n := len(payload.Alerts)
	if n == 0 {
		stats := s.storage.GetStats()
		s.metrics.SetActiveFiring(stats["firing"])
		s.metrics.SetActiveResolved(stats["resolved"])
		return nil, nil
	}

	results := make(chan singleAlertResult, n)
	sem := make(chan struct{}, alertWorkerCount)

	for i := range payload.Alerts {
		sem <- struct{}{}
		go func(idx int, alert alert_models.Alert) {
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("Panic in alert worker",
						zap.Int("index", idx),
						zap.String("fingerprint", alert.Fingerprint),
						zap.Any("panic", r))
					results <- singleAlertResult{
						index: idx,
						alert: &alert_models.ProcessedAlert{
							EnrichedAlert: alert_models.EnrichedAlert{
								Alert: alert,
							},
						},
					}
				}
			}()
			res := s.processSingleAlert(ctx, &alert)
			results <- res
		}(i, payload.Alerts[i])
	}

	processed := make([]*alert_models.ProcessedAlert, n)
	for range payload.Alerts {
		r := <-results
		if r.alert != nil {
			processed[r.index] = r.alert
		}
	}

	s.metrics.RecordProcessingLatency(time.Since(batchStart))

	if payload.Status == "resolved" && s.diagnosisCacheInvalidator != nil {
		for _, p := range processed {
			if p != nil {
				go func(fingerprint string) {
					if err := s.diagnosisCacheInvalidator.InvalidateDiagnosis(ctx, fingerprint); err != nil {
						s.logger.Warn("Failed to invalidate diagnosis cache",
							zap.String("fingerprint", fingerprint),
							zap.Error(err))
					}
				}(p.Fingerprint)
			}
		}
	}

	stats := s.storage.GetStats()
	s.metrics.SetActiveFiring(stats["firing"])
	s.metrics.SetActiveResolved(stats["resolved"])

	return processed, nil
}

func (s *AlertService) processSingleAlert(parentCtx context.Context, a *alert_models.Alert) singleAlertResult {
	start := time.Now()

	enrichCtx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel()

	enriched, err := s.enricher.Enrich(enrichCtx, a)
	if err != nil || enriched == nil {
		s.logger.Warn("Alert enrich failed or timed out, using fallback",
			zap.String("fingerprint", a.Fingerprint),
			zap.Error(err))
		enriched = &alert_models.EnrichedAlert{
			Alert:        *a,
			ResourceType: a.Labels["kind"],
			ResourceName: a.Labels["pod"],
			Namespace:    a.Labels["namespace"],
		}
		s.metrics.RecordEnrichmentDegraded(a.Fingerprint)
	}

	suppression, err := s.suppressor.CheckSuppression(parentCtx, enriched)
	if err != nil {
		s.logger.Error("Failed to check suppression",
			zap.String("fingerprint", a.Fingerprint),
			zap.Error(err))
		suppression = &alert_models.SuppressionResult{IsSuppressed: false}
	}

	if suppression.IsSuppressed {
		s.metrics.RecordSuppressed(suppression.SuppressionReason)
	}

	routing, err := s.router.Route(parentCtx, enriched)
	if err != nil {
		s.logger.Error("Failed to route alert",
			zap.String("fingerprint", a.Fingerprint),
			zap.Error(err))
		routing = &alert_models.RoutingResult{Receiver: "default"}
	}

	processed := &alert_models.ProcessedAlert{
		EnrichedAlert: *enriched,
		Suppression:   *suppression,
		Routing:       *routing,
	}

	if err := s.storage.Save(parentCtx, processed); err != nil {
		s.logger.Error("Failed to save alert",
			zap.String("fingerprint", a.Fingerprint),
			zap.Error(err))
	}

	if s.aggregator != nil {
		s.aggregator.Add(processed)
	}

	if !suppression.IsSuppressed {
		if err := s.notifier.Notify(parentCtx, processed); err != nil {
			s.logger.Error("Failed to notify alert",
				zap.String("fingerprint", a.Fingerprint),
				zap.Error(err))
		} else {
			s.metrics.RecordNotified(routing.NotifyChannel)
		}
	} else {
		s.logger.Info("Alert suppressed",
			zap.String("fingerprint", a.Fingerprint),
			zap.String("reason", suppression.SuppressionReason))
	}

	if s.autoDiagnosisPipeline != nil {
		go s.autoDiagnosisPipeline.OnAlertProcessed(parentCtx, processed)
	}

	s.metrics.RecordProcessingDuration(start, suppression.IsSuppressed)

	if s.db != nil {
		go func() {
			s.recordDecision(a.Fingerprint, "enrichment",
				fmt.Sprintf("kind=%s name=%s", a.Labels["kind"], a.Labels["pod"]),
				fmt.Sprintf("resource=%s/%s ns=%s biz=%s", enriched.ResourceType, enriched.ResourceName, enriched.Namespace, enriched.BusinessContext.AppName),
				"enriched", 0)
			dec := "allowed"
			if suppression.IsSuppressed {
				dec = "suppressed:" + suppression.SuppressionReason
			}
			s.recordDecision(a.Fingerprint, "suppression",
				fmt.Sprintf("%s/%s severity=%s", enriched.ResourceType, enriched.ResourceName, a.Labels["severity"]),
				dec, dec, 0)
			s.recordDecision(a.Fingerprint, "routing",
				fmt.Sprintf("severity=%s", routing.Severity),
				fmt.Sprintf("channel=%s receiver=%s", routing.NotifyChannel, routing.Receiver),
				"route_to_"+routing.NotifyChannel, 0)
			if !suppression.IsSuppressed {
				s.recordDecision(a.Fingerprint, "notification",
					fmt.Sprintf("channel=%s", routing.NotifyChannel),
					"notified", "notified", 0)
			}
		}()
	}

	return singleAlertResult{alert: processed}
}

func (s *AlertService) HandleWebhook(ctx context.Context, payload *alert_models.WebhookPayload) error {
	_, err := s.Process(ctx, payload)
	return err
}

func (s *AlertService) GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert_models.ProcessedAlert, error) {
	return s.storage.GetActiveAlerts(ctx, filters)
}

func (s *AlertService) GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert_models.ProcessedAlert, error) {
	return s.storage.GetAlertByFingerprint(ctx, fingerprint)
}

func (s *AlertService) recordDecision(fingerprint, nodeName, inputSummary, outputSummary, decision string, durationMs int64) {
	record := retrospective_model.DecisionRecord{
		Fingerprint:   fingerprint,
		NodeName:      nodeName,
		InputSummary:  inputSummary,
		OutputSummary: outputSummary,
		Decision:      decision,
		DurationMs:    durationMs,
	}
	if err := s.db.Create(&record).Error; err != nil {
		s.logger.Error("Failed to record decision",
			zap.String("fingerprint", fingerprint),
			zap.String("node", nodeName),
			zap.Error(err))
	}
}

func (s *AlertService) ProcessExternal(ctx context.Context, alert *alert_models.Alert, source *alert_models.AlertSource) ([]*alert_models.ProcessedAlert, error) {
	s.logger.Info("Processing external alert",
		zap.String("source", source.Name),
		zap.String("type", source.Type),
		zap.String("strategy", string(source.EnrichmentStrategy)),
		zap.String("fingerprint", alert.Fingerprint))

	s.metrics.RecordReceived(alert.Status)

	enrichCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	enriched := s.enrichExternal(enrichCtx, alert, source)

	var suppression alert_models.SuppressionResult
	if s.suppressor != nil {
		result, err := s.suppressor.CheckSuppression(ctx, enriched)
		if err != nil {
			s.logger.Warn("External alert suppression check failed", zap.Error(err))
		}
		suppression = *result
	}

	var routing alert_models.RoutingResult
	if s.router != nil {
		result, err := s.router.Route(ctx, enriched)
		if err != nil {
			s.logger.Warn("External alert routing failed", zap.Error(err))
		}
		routing = *result
	}

	processed := &alert_models.ProcessedAlert{
		EnrichedAlert: *enriched,
		Suppression:   suppression,
		Routing:       routing,
		ProcessedAt:   time.Now(),
	}

	if s.storage != nil {
		if err := s.storage.Save(ctx, processed); err != nil {
			s.logger.Error("Failed to save external alert", zap.Error(err))
		}
	}

	if !suppression.IsSuppressed && s.notifier != nil {
		if err := s.notifier.Notify(ctx, processed); err != nil {
			s.logger.Error("Failed to notify external alert", zap.Error(err))
		} else {
			s.metrics.RecordNotified(alert.Status)
		}
	} else if suppression.IsSuppressed {
		s.metrics.RecordSuppressed(suppression.SuppressionReason)
		s.logger.Info("External alert suppressed",
			zap.String("fingerprint", alert.Fingerprint),
			zap.String("reason", suppression.SuppressionReason))
	}

	if s.aggregator != nil {
		s.aggregator.Add(processed)
	}

	if s.autoDiagnosisPipeline != nil && !suppression.IsSuppressed {
		s.autoDiagnosisPipeline.OnAlertProcessed(ctx, processed)
	}

	s.logger.Info("External alert processed successfully",
		zap.String("source", source.Name),
		zap.String("fingerprint", alert.Fingerprint),
		zap.Bool("suppressed", suppression.IsSuppressed))

	return []*alert_models.ProcessedAlert{processed}, nil
}

func (s *AlertService) enrichExternal(ctx context.Context, alert *alert_models.Alert, source *alert_models.AlertSource) *alert_models.EnrichedAlert {
	k8sLabels := alert_models.ExtractK8sLabels(alert.Labels)

	enriched := &alert_models.EnrichedAlert{
		Alert:         *alert,
		Namespace:     k8sLabels.Namespace,
		ResourceName:  alert.Labels["node"],
		ResourceType:  "Unknown",
		ResourceUID:   k8sLabels.ResourceUID,
		EnrichTags:    make(map[string]string),
		TopologyPath:  []string{},
		RelatedAlerts: []string{},
	}

	enriched.EnrichTags["alertSource"] = source.Name
	enriched.EnrichTags["alertSourceType"] = source.Type

	if ae, ok := s.enricher.(*AlertEnricher); ok {
		ae.EnrichFromExternal(ctx, enriched, source)
	} else {
		s.logger.Warn("Enricher is not AlertEnricher, external enrichment skipped")
	}

	return enriched
}
