package diagnosis

import (
	"context"
	"sync/atomic"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CallCount        int `json:"call_count"`
}

type TokenTrackingChatModel struct {
	inner           model.BaseChatModel
	logger          *zap.Logger
	totalPrompt     atomic.Int64
	totalCompletion atomic.Int64
	totalTokens     atomic.Int64
	callCount       atomic.Int64
}

func NewTokenTrackingChatModel(inner model.BaseChatModel, logger *zap.Logger) *TokenTrackingChatModel {
	return &TokenTrackingChatModel{inner: inner, logger: logger}
}

func (t *TokenTrackingChatModel) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := t.inner.Generate(ctx, msgs, opts...)
	if err != nil {
		return resp, err
	}
	t.callCount.Add(1)
	if resp != nil && resp.ResponseMeta != nil && resp.ResponseMeta.Usage != nil {
		usage := resp.ResponseMeta.Usage
		t.totalPrompt.Add(int64(usage.PromptTokens))
		t.totalCompletion.Add(int64(usage.CompletionTokens))
		t.totalTokens.Add(int64(usage.TotalTokens))
		t.logger.Info(
			"LLM call completed",
			zap.Int("prompt_tokens", usage.PromptTokens),
			zap.Int("completion_tokens", usage.CompletionTokens),
			zap.Int("total_tokens", usage.TotalTokens),
			zap.Int64("cumulative_calls", t.callCount.Load()),
			zap.Int64("cumulative_total_tokens", t.totalTokens.Load()),
		)
	}
	return resp, nil
}

func (t *TokenTrackingChatModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return t.inner.Stream(ctx, msgs, opts...)
}

func (t *TokenTrackingChatModel) TokenUsage() TokenUsage {
	return TokenUsage{
		PromptTokens:     int(t.totalPrompt.Load()),
		CompletionTokens: int(t.totalCompletion.Load()),
		TotalTokens:      int(t.totalTokens.Load()),
		CallCount:        int(t.callCount.Load()),
	}
}

var _ model.BaseChatModel = (*TokenTrackingChatModel)(nil)
