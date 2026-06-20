package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type RedisStorage struct {
	client  *redis.Client
	logger  interfaces.Logger
	ttl     time.Duration
	pgStore *PostgresStorage
}

func NewRedisStorage(addr string, password string, db int, ttl time.Duration, logger interfaces.Logger) *RedisStorage {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		PoolSize: 20,
	})

	if password == "" {
		logger.Warn("Redis password is empty — diagnosis session data and cached diagnosis results will be stored without authentication",
			zap.String("hint", "Set a password in config.diagnosis.yaml session.password or MUTONG_REDIS_PASSWORD env var"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("Redis connection failed, will retry on first use", zap.String("addr", addr), zap.Error(err))
	}

	return &RedisStorage{
		client:  client,
		logger:  logger,
		ttl:     ttl,
		pgStore: nil,
	}
}

func (r *RedisStorage) WithPostgresStorage(pgStore *PostgresStorage) *RedisStorage {
	r.pgStore = pgStore
	return r
}

func (r *RedisStorage) CreateSession(ctx context.Context, session *ChatSession) error {
	pipe := r.client.Pipeline()

	sessionKey := r.sessionKey(session.ID)

	alertFP := ""
	resourceKind := ""
	resourceName := ""
	namespace := ""
	if session.Context != nil {
		if session.Context.Alert != nil {
			alertFP = session.Context.Alert.Fingerprint
		}
		resourceKind = session.Context.ResourceKind
		resourceName = session.Context.ResourceName
		namespace = session.Context.Namespace
	}

	pipe.HSet(ctx, sessionKey, map[string]interface{}{
		"id":                session.ID,
		"status":            "active",
		"alert_fingerprint": alertFP,
		"resource_kind":     resourceKind,
		"resource_name":     resourceName,
		"namespace":         namespace,
		"context":           r.serializeContext(session.Context),
		"created_at":        session.CreatedAt.Unix(),
		"last_active":       session.LastActive.Unix(),
	})
	pipe.Expire(ctx, sessionKey, r.ttl)

	if alertFP != "" {
		alertIndexKey := r.alertIndexKey(alertFP)
		pipe.SAdd(ctx, alertIndexKey, session.ID)
		pipe.Expire(ctx, alertIndexKey, 24*time.Hour)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		r.logger.Error("Redis CreateSession failed", zap.Error(err))
		return fmt.Errorf("redis create session: %w", err)
	}

	r.logger.Info("Session created in Redis", zap.String("session_id", session.ID))
	return nil
}

func (r *RedisStorage) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	sessionKey := r.sessionKey(id)
	vals, err := r.client.HGetAll(ctx, sessionKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis get session: %w", err)
	}

	if len(vals) == 0 {
		return nil, fmt.Errorf("session not found")
	}

	session := &ChatSession{
		ID:         vals["id"],
		CreatedAt:  time.Unix(parseInt64(vals["created_at"]), 0),
		LastActive: time.Unix(parseInt64(vals["last_active"]), 0),
		SSEChan:    make(chan *SSEEvent, 100),
	}

	if ctxData, ok := vals["context"]; ok {
		session.Context = r.deserializeContext(ctxData)
	}

	messagesKey := r.messagesKey(id)
	messages, err := r.client.LRange(ctx, messagesKey, 0, -1).Result()
	if err == nil {
		for _, msgJSON := range messages {
			var msg ChatMessage
			if err := json.Unmarshal([]byte(msgJSON), &msg); err == nil {
				session.Messages = append(session.Messages, msg)
			}
		}
	}

	return session, nil
}

func (r *RedisStorage) GetSessionByAlert(ctx context.Context, fingerprint string) (*ChatSession, error) {
	alertIndexKey := r.alertIndexKey(fingerprint)
	sessionIDs, err := r.client.SMembers(ctx, alertIndexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis get session by alert: %w", err)
	}

	if len(sessionIDs) == 0 {
		return nil, fmt.Errorf("no session found for alert")
	}

	return r.GetSession(ctx, sessionIDs[len(sessionIDs)-1])
}

func (r *RedisStorage) AppendMessage(ctx context.Context, sessionID string, msg ChatMessage) error {
	messagesKey := r.messagesKey(sessionID)
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	pipe := r.client.Pipeline()
	pipe.RPush(ctx, messagesKey, msgJSON)
	pipe.Expire(ctx, messagesKey, r.ttl)

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisStorage) UpdateLastActive(ctx context.Context, id string) error {
	sessionKey := r.sessionKey(id)
	pipe := r.client.Pipeline()
	pipe.HSet(ctx, sessionKey, "last_active", time.Now().Unix())
	pipe.Expire(ctx, sessionKey, r.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisStorage) DeleteSession(ctx context.Context, id string) error {
	pipe := r.client.Pipeline()

	sessionKey := r.sessionKey(id)
	session, err := r.GetSession(ctx, id)
	if err == nil && session.Context != nil && session.Context.Alert != nil {
		alertIndexKey := r.alertIndexKey(session.Context.Alert.Fingerprint)
		pipe.SRem(ctx, alertIndexKey, id)
	}

	pipe.Del(ctx, sessionKey)
	pipe.Del(ctx, r.messagesKey(id))

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisStorage) sessionKey(id string) string {
	return fmt.Sprintf("sess:%s", id)
}

func (r *RedisStorage) messagesKey(id string) string {
	return fmt.Sprintf("sess:%s:messages", id)
}

func (r *RedisStorage) alertIndexKey(fingerprint string) string {
	return fmt.Sprintf("alert:fp:%s", fingerprint)
}

func (r *RedisStorage) serializeContext(ctx *DiagnosisContext) string {
	if ctx == nil {
		return ""
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		return ""
	}
	return string(data)
}

func (r *RedisStorage) deserializeContext(data string) *DiagnosisContext {
	var ctx DiagnosisContext
	if err := json.Unmarshal([]byte(data), &ctx); err != nil {
		return nil
	}
	return &ctx
}

func parseInt64(s string) int64 {
	var n int64
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// -----------------------------------------------------------------------
// Diagnosis result cache
// -----------------------------------------------------------------------

func (r *RedisStorage) diagnosisKey(fingerprint string) string {
	return fmt.Sprintf("diagnosis:fp:%s", fingerprint)
}

func (r *RedisStorage) diagnosisMetaKey(fingerprint string) string {
	return fmt.Sprintf("diagnosis:fp:%s:meta", fingerprint)
}

// CacheDiagnosisResult 将诊断结果写入 Redis 缓存
func (r *RedisStorage) CacheDiagnosisResult(ctx context.Context, fingerprint string, result *diagnosis.DiagnosisResult, ttl time.Duration) error {
	key := r.diagnosisKey(fingerprint)
	metaKey := r.diagnosisMetaKey(fingerprint)

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal diagnosis result: %w", err)
	}

	source := "llm"
	if len(result.RootCauses) > 0 && result.RootCauses[0].Confidence < 0.8 {
		source = "rule"
	}

	meta := map[string]interface{}{
		"created_at": time.Now().Unix(),
		"source":     source,
		"version":    1,
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, data, ttl)
	pipe.HSet(ctx, metaKey, meta)
	pipe.Expire(ctx, metaKey, ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		r.logger.Error("Cache diagnosis result failed", zap.Error(err))
		return fmt.Errorf("cache diagnosis result: %w", err)
	}

	r.logger.Info("Diagnosis result cached", zap.String("fingerprint", fingerprint), zap.Duration("ttl", ttl))
	return nil
}

// GetCachedDiagnosisResult 从 Redis 获取缓存的诊断结果，未命中时回退到 PostgreSQL
// 返回 (nil, nil) 表示缓存未命中（非错误）
func (r *RedisStorage) GetCachedDiagnosisResult(ctx context.Context, fingerprint string) (*diagnosis.DiagnosisResult, error) {
	key := r.diagnosisKey(fingerprint)
	val, err := r.client.Get(ctx, key).Result()
	if err == nil {
		var result diagnosis.DiagnosisResult
		if err := json.Unmarshal([]byte(val), &result); err != nil {
			return nil, fmt.Errorf("unmarshal cached diagnosis: %w", err)
		}
		return &result, nil
	}

	if err != redis.Nil {
		r.logger.Warn("Redis get cached diagnosis error, falling back to PG",
			zap.String("fingerprint", fingerprint),
			zap.Error(err))
	}

	if r.pgStore != nil {
		result, pgErr := r.pgStore.GetDiagnosisResult(ctx, fingerprint)
		if pgErr == nil && result != nil {
			go func() {
				bgCtx := context.Background()
				if cacheErr := r.CacheDiagnosisResult(bgCtx, fingerprint, result, r.ttl); cacheErr != nil {
					r.logger.Warn("Failed to warm Redis cache from PG",
						zap.String("fingerprint", fingerprint),
						zap.Error(cacheErr))
				}
			}()
			return result, nil
		}
	}

	return nil, nil
}

// InvalidateDiagnosis 清除指定告警的诊断缓存
func (r *RedisStorage) InvalidateDiagnosis(ctx context.Context, fingerprint string) error {
	pipe := r.client.Pipeline()
	pipe.Del(ctx, r.diagnosisKey(fingerprint))
	pipe.Del(ctx, r.diagnosisMetaKey(fingerprint))
	_, err := pipe.Exec(ctx)
	return err
}
