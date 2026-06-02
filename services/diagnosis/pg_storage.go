package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/tddh/mutong/interfaces"
	alert_models "gitee.com/tddh/mutong/models/alert"
	diagnosis_model "gitee.com/tddh/mutong/models/diagnosis"
)

type PostgresStorage struct {
	db     *gorm.DB
	logger interfaces.Logger
}

func NewPostgresStorage(db *gorm.DB, logger interfaces.Logger) *PostgresStorage {
	return &PostgresStorage{
		db:     db,
		logger: logger,
	}
}

func (p *PostgresStorage) ArchiveSession(ctx context.Context, session *ChatSession) error {
	sessionModel := &diagnosis_model.ChatSessionModel{
		ID:           session.ID,
		Status:       1,
		CreatedAt:    session.CreatedAt,
		LastActiveAt: session.LastActive,
	}

	if session.Context != nil {
		if session.Context.Alert != nil {
			sessionModel.AlertFingerprint = session.Context.Alert.Fingerprint
		}
		sessionModel.ResourceKind = session.Context.ResourceKind
		sessionModel.ResourceName = session.Context.ResourceName
		sessionModel.Namespace = session.Context.Namespace
	}

	if err := p.db.WithContext(ctx).Create(sessionModel).Error; err != nil {
		p.logger.Error("MySQL ArchiveSession failed", zap.Error(err))
		return fmt.Errorf("mysql archive session: %w", err)
	}

	if session.Context != nil {
		contextModel := &diagnosis_model.DiagnosisContextModel{
			SessionID: session.ID,
			CreatedAt: session.CreatedAt,
		}

		if session.Context.Alert != nil {
			alertInfoJSON, _ := json.Marshal(session.Context.Alert)
			s := string(alertInfoJSON)
			contextModel.AlertInfo = &s
		}

		if session.Context.Topology != nil {
			topologyRefJSON, _ := json.Marshal(map[string]interface{}{
				"resource_kind": session.Context.ResourceKind,
				"resource_name": session.Context.ResourceName,
				"namespace":     session.Context.Namespace,
			})
			s := string(topologyRefJSON)
			contextModel.TopologyRef = &s
		}

		if len(session.Context.Metrics) > 0 {
			metricsJSON, _ := json.Marshal(session.Context.Metrics)
			s := string(metricsJSON)
			contextModel.MetricsQueries = &s
		}

		if len(session.Context.Logs) > 0 {
			logsJSON, _ := json.Marshal(session.Context.Logs)
			s := string(logsJSON)
			contextModel.LogsQueries = &s
		}

		if err := p.db.WithContext(ctx).Create(contextModel).Error; err != nil {
			p.logger.Warn("MySQL archive context failed", zap.Error(err))
		}
	}

	p.logger.Info("Session archived to MySQL", zap.String("session_id", session.ID))
	return nil
}

func (p *PostgresStorage) ArchiveMessages(ctx context.Context, sessionID string, messages []ChatMessage) error {
	if len(messages) == 0 {
		return nil
	}

	var messageModels []diagnosis_model.ChatMessageModel
	for _, msg := range messages {
		messageModels = append(messageModels, diagnosis_model.ChatMessageModel{
			SessionID: sessionID,
			MsgID:     msg.ID,
			Role:      msg.Role,
			Content:   msg.Content,
			CreatedAt: msg.Timestamp,
		})
	}

	if err := p.db.WithContext(ctx).Create(&messageModels).Error; err != nil {
		p.logger.Error("MySQL ArchiveMessages failed", zap.Error(err))
		return fmt.Errorf("mysql archive messages: %w", err)
	}

	p.logger.Info("Messages archived to MySQL", zap.String("session_id", sessionID), zap.Int("count", len(messages)))
	return nil
}

func (p *PostgresStorage) GetSessionByAlert(ctx context.Context, fingerprint string) (*ChatSession, error) {
	var sessionModel diagnosis_model.ChatSessionModel
	if err := p.db.WithContext(ctx).
		Where("alert_fingerprint = ? AND status = ?", fingerprint, 1).
		Order("created_at DESC").
		First(&sessionModel).Error; err != nil {
		return nil, fmt.Errorf("mysql get session by alert: %w", err)
	}

	session := &ChatSession{
		ID:         sessionModel.ID,
		CreatedAt:  sessionModel.CreatedAt,
		LastActive: sessionModel.LastActiveAt,
		SSEChan:    make(chan *SSEEvent, 100),
		Context: &DiagnosisContext{
			ResourceKind: sessionModel.ResourceKind,
			ResourceName: sessionModel.ResourceName,
			Namespace:    sessionModel.Namespace,
		},
	}

	var contextModel diagnosis_model.DiagnosisContextModel
	if err := p.db.WithContext(ctx).Where("session_id = ?", sessionModel.ID).First(&contextModel).Error; err == nil {
		if contextModel.AlertInfo != nil && *contextModel.AlertInfo != "" {
			var alertInfo alert_models.ProcessedAlert
			if err := json.Unmarshal([]byte(*contextModel.AlertInfo), &alertInfo); err == nil {
				session.Context.Alert = &alertInfo
			}
		}
	}

	var messageModels []diagnosis_model.ChatMessageModel
	if err := p.db.WithContext(ctx).
		Where("session_id = ?", sessionModel.ID).
		Order("created_at ASC").
		Find(&messageModels).Error; err == nil {
		for _, msgModel := range messageModels {
			session.Messages = append(session.Messages, ChatMessage{
				ID:        msgModel.MsgID,
				Role:      msgModel.Role,
				Content:   msgModel.Content,
				Timestamp: msgModel.CreatedAt,
			})
		}
	}

	return session, nil
}

func (p *PostgresStorage) UpdateSessionStatus(ctx context.Context, sessionID string, status int8) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if status == 2 {
		now := time.Now()
		updates["closed_at"] = &now
	}

	if err := p.db.WithContext(ctx).
		Model(&diagnosis_model.ChatSessionModel{}).
		Where("id = ?", sessionID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("mysql update session status: %w", err)
	}

	return nil
}

func (p *PostgresStorage) CleanupExpired(ctx context.Context, ttl time.Duration) error {
	expiredAt := time.Now().Add(-ttl)

	var expiredSessions []diagnosis_model.ChatSessionModel
	if err := p.db.WithContext(ctx).
		Where("last_active_at < ? AND status = ?", expiredAt, 1).
		Find(&expiredSessions).Error; err != nil {
		return fmt.Errorf("mysql find expired sessions: %w", err)
	}

	if len(expiredSessions) == 0 {
		return nil
	}

	sessionIDs := make([]string, 0, len(expiredSessions))
	for _, s := range expiredSessions {
		sessionIDs = append(sessionIDs, s.ID)
	}

	p.db.WithContext(ctx).Model(&diagnosis_model.ChatSessionModel{}).Where("id IN ?", sessionIDs).Update("status", 3)
	p.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Delete(&diagnosis_model.ChatMessageModel{})
	p.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Delete(&diagnosis_model.DiagnosisContextModel{})

	p.logger.Info("Cleaned up expired sessions from MySQL", zap.Int("count", len(expiredSessions)))
	return nil
}

func (p *PostgresStorage) SaveDiagnosisResult(ctx context.Context, fingerprint string, result *diagnosis_model.DiagnosisResult, source string) error {
	jsonData, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal diagnosis result: %w", err)
	}

	confidence := 0.0
	if len(result.RootCauses) > 0 {
		confidence = result.RootCauses[0].Confidence
	}
	if source == "" {
		source = "llm"
		if confidence < 0.8 && len(result.RootCauses) > 0 {
			source = "rule"
		}
	}

	model := diagnosis_model.DiagnosisResultModel{
		Fingerprint: fingerprint,
		ResultJSON:  string(jsonData),
		Source:      source,
		Confidence:  confidence,
	}

	if err := p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "fingerprint"}},
		DoUpdates: clause.AssignmentColumns([]string{"result_json", "source", "confidence", "updated_at"}),
	}).Create(&model).Error; err != nil {
		p.logger.Error("Save diagnosis result failed", zap.Error(err))
		return fmt.Errorf("save diagnosis result: %w", err)
	}

	p.logger.Info("Diagnosis result saved to PG", zap.String("fingerprint", fingerprint))
	return nil
}

func (p *PostgresStorage) GetDiagnosisResult(ctx context.Context, fingerprint string) (*diagnosis_model.DiagnosisResult, error) {
	var model diagnosis_model.DiagnosisResultModel
	if err := p.db.WithContext(ctx).
		Where("fingerprint = ?", fingerprint).
		First(&model).Error; err != nil {
		return nil, fmt.Errorf("get diagnosis result: %w", err)
	}

	var result diagnosis_model.DiagnosisResult
	if err := json.Unmarshal([]byte(model.ResultJSON), &result); err != nil {
		return nil, fmt.Errorf("unmarshal diagnosis result: %w", err)
	}

	return &result, nil
}

func (p *PostgresStorage) CleanupOldResults(ctx context.Context, retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := p.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&diagnosis_model.DiagnosisResultModel{})

	if result.Error != nil {
		return fmt.Errorf("cleanup old diagnosis results: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		p.logger.Info("Cleaned up expired diagnosis results",
			zap.Int64("count", result.RowsAffected),
			zap.Int("retention_days", retentionDays))
	}
	return nil
}
