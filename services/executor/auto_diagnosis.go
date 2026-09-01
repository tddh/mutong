package executor

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	diagnosisPkg "gitee.com/tddh/mutong/models/diagnosis"
	ex "gitee.com/tddh/mutong/models/executor"
	diagEngine "gitee.com/tddh/mutong/services/diagnosis"
)

// AutoDiagnosisPipeline watches for processed alerts and triggers diagnosis -> remediation
type AutoDiagnosisPipeline struct {
	logger       interfaces.Logger
	engine       *diagEngine.Engine
	bridge       *RemediationBridge
	executor     interfaces.Executor
	executorMu   sync.RWMutex
	alertQueue   chan *alert_models.ProcessedAlert
	enabled      bool
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	stopped      atomic.Bool
	redisStorage *diagEngine.RedisStorage
	pgStorage    *diagEngine.PostgresStorage
	cacheTTL     time.Duration
}

// NewAutoDiagnosisPipeline creates a new auto-diagnosis pipeline instance
func NewAutoDiagnosisPipeline(logger interfaces.Logger, engine *diagEngine.Engine, bridge *RemediationBridge, executor interfaces.Executor, enabled bool) *AutoDiagnosisPipeline {
	return &AutoDiagnosisPipeline{
		logger:     logger,
		engine:     engine,
		bridge:     bridge,
		executor:   executor,
		alertQueue: make(chan *alert_models.ProcessedAlert, 100),
		enabled:    enabled,
	}
}

// SetExecutor updates the executor used by the pipeline
func (p *AutoDiagnosisPipeline) SetExecutor(executor interfaces.Executor) {
	p.executorMu.Lock()
	defer p.executorMu.Unlock()
	p.executor = executor
}

// SetDiagnosisResultStore 注入诊断结果存储，让自动诊断结果可被前端复用（否则点开告警会现场重新诊断）
func (p *AutoDiagnosisPipeline) SetDiagnosisResultStore(redis *diagEngine.RedisStorage, pg *diagEngine.PostgresStorage, cacheTTL time.Duration) {
	p.redisStorage = redis
	p.pgStorage = pg
	p.cacheTTL = cacheTTL
}

func (p *AutoDiagnosisPipeline) getExecutor() interfaces.Executor {
	p.executorMu.RLock()
	defer p.executorMu.RUnlock()
	return p.executor
}

// OnAlertProcessed is the callback invoked when an alert has been processed by the alert pipeline
func (p *AutoDiagnosisPipeline) OnAlertProcessed(ctx context.Context, alert *alert_models.ProcessedAlert) {
	if !p.enabled || p.stopped.Load() {
		return
	}
	if alert == nil {
		return
	}
	select {
	case p.alertQueue <- alert:
	default:
		if p.logger != nil {
			p.logger.Warn("AutoDiagnosis queue full, dropping alert for auto-diagnosis", zap.String("fingerprint", alert.Fingerprint))
		}
	}
}

// ShouldAutoDiagnose determines if the given alert should trigger auto-diagnosis
func (p *AutoDiagnosisPipeline) ShouldAutoDiagnose(alert *alert_models.ProcessedAlert) bool {
	if alert == nil {
		return false
	}
	// 1. status firing
	if alert.Status != "firing" {
		return false
	}
	// 2. severity must be warning or critical
	sev := strings.ToLower(alert.Routing.Severity)
	if sev != "warning" && sev != "critical" {
		return false
	}
	// 3. not suppressed
	if alert.Suppression.IsSuppressed {
		return false
	}
	// 4. recognizable resource type
	rt := alert.ResourceType
	if rt != "Pod" && rt != "Node" && rt != "Deployment" {
		return false
	}
	return true
}

// Start launches a background worker to process incoming alerts
func (p *AutoDiagnosisPipeline) Start(ctx context.Context) {
	if !p.enabled {
		return
	}
	// Use a child context so we can cancel this pipeline independently
	childCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			select {
			case a, ok := <-p.alertQueue:
				if !ok {
					return
				}
				p.processAlert(childCtx, a)
			case <-childCtx.Done():
				return
			}
		}
	}()
}

// Stop stops the background processing
func (p *AutoDiagnosisPipeline) Stop() {
	if !p.stopped.CompareAndSwap(false, true) {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	close(p.alertQueue)
	p.wg.Wait()
}

// processAlert runs the diagnosis and potential remediation for a single alert
func (p *AutoDiagnosisPipeline) processAlert(ctx context.Context, alert *alert_models.ProcessedAlert) {
	if alert == nil {
		return
	}
	// Should we diagnose this alert?
	if !p.ShouldAutoDiagnose(alert) {
		return
	}
	// Build a diagnosis request
	req := diagnosisPkg.DiagnosisRequest{
		Fingerprint: alert.Fingerprint,
		Namespace:   alert.Namespace,
		Resource:    alert.ResourceType,
	}

	// Run in a separate goroutine so as not to block this worker
	go func() {
		res, err := p.engine.Diagnose(ctx, req)
		if err != nil {
			if p.logger != nil {
				p.logger.Warn("Auto-diagnosis diagnosis failed", zap.Error(err))
			}
			return
		}
		if res == nil {
			return
		}

		if p.redisStorage != nil {
			if err := p.redisStorage.CacheDiagnosisResult(ctx, alert.Fingerprint, res, p.cacheTTL); err != nil && p.logger != nil {
				p.logger.Warn("Auto-diagnosis failed to cache diagnosis result", zap.String("fingerprint", alert.Fingerprint), zap.Error(err))
			}
		}
		if p.pgStorage != nil {
			if err := p.pgStorage.SaveDiagnosisResult(ctx, alert.Fingerprint, res, ""); err != nil && p.logger != nil {
				p.logger.Warn("Auto-diagnosis failed to persist diagnosis result", zap.String("fingerprint", alert.Fingerprint), zap.Error(err))
			}
		}

		exec := p.getExecutor()
		autoMode := false
		if exec != nil {
			autoMode = exec.IsAutoMode()
		}
		plan, shouldAuto := p.bridge.CreatePlanFromDiagnosis(res, autoMode)
		if plan == nil {
			return
		}
		plan.Fingerprint = alert.Fingerprint
		if shouldAuto {
			if exec != nil {
				if _, err := p.bridge.ExecuteRemediation(ctx, plan, exec); err != nil {
					if p.logger != nil {
						p.logger.Error("Auto-diagnosis remediation execution failed", zap.Error(err))
					}
				}
			} else {
				if p.logger != nil {
					p.logger.Warn("Auto-diagnosis cannot execute: executor not configured", zap.String("plan_id", plan.ID))
				}
			}
		} else {
			if exec != nil {
				audit := ex.AuditLog{
					ID:           generateAuditID(),
					Plan:         *plan,
					Result:       ex.ExecutionResult{Success: false, Message: "pending approval (not auto-executed)", Timestamp: time.Now()},
					AutoExecuted: false,
					ApprovedBy:   "",
					Timestamp:    time.Now(),
				}
				if err := exec.RecordAudit(ctx, audit); err != nil && p.logger != nil {
					p.logger.Warn("Auto-diagnosis failed to record pending plan", zap.String("plan_id", plan.ID), zap.Error(err))
				}
			} else {
				if p.logger != nil {
					p.logger.Info("Auto-diagnosis produced remediation plan but manual approval required", zap.String("plan_id", plan.ID))
				}
			}
		}
	}()
}

// No-op: keep file self-contained without forcing additional dependencies
