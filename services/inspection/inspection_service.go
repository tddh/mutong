package inspection

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type InspectionService struct {
	logger   interfaces.Logger
	engine   interfaces.InspectionEngine
	reporter interfaces.InspectionReporter
	cron     *cron.Cron
}

func NewInspectionService(
	logger interfaces.Logger,
	engine interfaces.InspectionEngine,
	reporter interfaces.InspectionReporter,
	cronSpec string,
) interfaces.InspectionProcessor {
	s := &InspectionService{
		logger:   logger,
		engine:   engine,
		reporter: reporter,
	}

	if cronSpec != "" {
		s.cron = cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))
		_, _ = s.cron.AddFunc(cronSpec, func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("Inspection panic recovered", zap.Any("recover", r))
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_, _ = s.runInspection(ctx)
		})
	}

	return s
}

func (s *InspectionService) Start() error {
	s.logger.Info("Starting inspection service")
	if s.cron != nil {
		s.cron.Start()
		s.logger.Info("Inspection scheduler started")
	}
	return nil
}

func (s *InspectionService) Stop() {
	if s.cron != nil {
		s.cron.Stop()
	}
	s.logger.Info("Inspection service stopped")
}

func (s *InspectionService) ExecuteNow(ctx context.Context) ([]inspection.InspectionResult, error) {
	return s.runInspection(ctx)
}

func (s *InspectionService) GetLatestReport() (*inspection.InspectionReport, error) {
	return s.reporter.GetLatestReport()
}

func (s *InspectionService) GetReportByID(id string) (*inspection.InspectionReport, error) {
	return s.reporter.GetReportByID(id)
}

func (s *InspectionService) GetReportsByRange(start, end time.Time, limit, offset int) ([]*inspection.InspectionReport, error) {
	return s.reporter.GetReportsByRange(start, end, limit, offset)
}

func (s *InspectionService) GetTrendData(days int) ([]inspection.TrendPoint, error) {
	return s.reporter.GetTrendData(days)
}

func (s *InspectionService) CompareReports(a, b *inspection.InspectionReport) *inspection.ComparisonResult {
	return CompareReports(a, b)
}

func (s *InspectionService) runInspection(ctx context.Context) ([]inspection.InspectionResult, error) {
	s.logger.Debug("Running inspection")

	results, err := s.engine.ExecuteAll(ctx)
	if err != nil {
		s.logger.Error("Inspection failed", zap.Error(err))
		return nil, err
	}

	s.reporter.GenerateReport(results)

	s.logger.Debug("Inspection completed", zap.Int("issues", len(results)))
	return results, nil
}
