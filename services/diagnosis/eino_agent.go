package diagnosis

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	cbt "github.com/cloudwego/eino/utils/callbacks"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

type DiagnosisAgent struct {
	chatModel model.BaseChatModel
	tools     []tool.InvokableTool
	timeout   time.Duration
	maxSteps  int
	logger    interfaces.Logger
}

type AgentResult struct {
	Content     string
	TotalTokens int
}

type ToolCallInfo struct {
	Name      string
	Arguments string
}

func NewDiagnosisAgent(chatModel model.BaseChatModel, tools []tool.InvokableTool, logger interfaces.Logger) *DiagnosisAgent {
	return &DiagnosisAgent{
		chatModel: chatModel,
		tools:     tools,
		timeout:   120 * time.Second,
		maxSteps:  10,
		logger:    logger,
	}
}

func (a *DiagnosisAgent) Run(ctx context.Context, userMessage string, systemPrompt string) (*AgentResult, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	a.logger.Info(
		"DiagnosisAgent Run started",
		zap.Int("tools", len(a.tools)),
		zap.Int("max_steps", a.maxSteps),
	)

	var baseTools []tool.BaseTool
	for _, t := range a.tools {
		baseTools = append(baseTools, t)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "diagnosis_agent",
		Description:   "Kubernetes AI diagnosis agent",
		Instruction:   systemPrompt,
		Model:         a.chatModel,
		MaxIterations: a.maxSteps,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: baseTools,
			},
		},
	})
	if err != nil {
		a.logger.Error("Failed to create diagnosis agent", zap.Error(err))
		return nil, fmt.Errorf("create diagnosis agent: %w", err)
	}

	input := &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage(userMessage),
		},
	}

	startTime := time.Now()
	iter := agent.Run(ctx, input)

	var finalContent string
	stepCount := 0
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		stepCount++
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg := event.Output.MessageOutput.Message
			if msg != nil && msg.Role == schema.Assistant && msg.Content != "" {
				finalContent = msg.Content
				// 记录 LLM 每轮输出长度
				a.logger.Debug(
					"Agent step output",
					zap.Int("step", stepCount),
					zap.Int("content_len", len(msg.Content)),
				)
			}
			// 记录工具调用
			if msg != nil && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					a.logger.Info(
						"Agent tool call",
						zap.Int("step", stepCount),
						zap.String("tool", tc.Function.Name),
					)
				}
			}
		}
	}

	if finalContent == "" {
		a.logger.Error(
			"Agent returned no content",
			zap.Int("steps", stepCount),
			zap.Duration("duration", time.Since(startTime)),
		)
		return nil, fmt.Errorf("agent returned no content")
	}

	a.logger.Info(
		"DiagnosisAgent Run completed",
		zap.Int("steps", stepCount),
		zap.Int("content_len", len(finalContent)),
		zap.Duration("duration", time.Since(startTime)),
	)
	return &AgentResult{Content: finalContent}, nil
}

func (a *DiagnosisAgent) RunStream(ctx context.Context, userMessage string, systemPrompt string, callback func(chunk string, toolCall *ToolCallInfo) error) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	a.logger.Info(
		"DiagnosisAgent RunStream started",
		zap.Int("tools", len(a.tools)),
		zap.Int("max_steps", a.maxSteps),
	)

	var baseTools []tool.BaseTool
	for _, t := range a.tools {
		baseTools = append(baseTools, t)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "diagnosis_agent",
		Description:   "Kubernetes AI diagnosis agent",
		Instruction:   systemPrompt,
		Model:         a.chatModel,
		MaxIterations: a.maxSteps,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: baseTools,
			},
		},
	})
	if err != nil {
		a.logger.Error("Failed to create diagnosis agent", zap.Error(err))
		return fmt.Errorf("create diagnosis agent: %w", err)
	}

	input := &adk.AgentInput{
		Messages: []*schema.Message{
			schema.UserMessage(userMessage),
		},
		EnableStreaming: true,
	}

	startTime := time.Now()
	toolCallCount := 0
	totalContentLen := 0
	iter := agent.Run(ctx, input)

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			msgOut := event.Output.MessageOutput
			if msgOut.MessageStream != nil {
				stream := msgOut.MessageStream
				for {
					chunk, err := stream.Recv()
					if err != nil {
						break
					}
					if chunk.Content != "" {
						totalContentLen += len(chunk.Content)
						_ = callback(chunk.Content, nil)
					}
					for _, tc := range chunk.ToolCalls {
						if tc.Function.Name == "" {
							continue
						}
						toolCallCount++
						a.logger.Info(
							"Agent stream tool call",
							zap.Int("call_num", toolCallCount),
							zap.String("tool", tc.Function.Name),
						)
						_ = callback("", &ToolCallInfo{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						})
					}
				}
			} else if msgOut.Message != nil && msgOut.Message.Content != "" {
				totalContentLen += len(msgOut.Message.Content)
				_ = callback(msgOut.Message.Content, nil)
			}
		}
	}

	a.logger.Info(
		"DiagnosisAgent RunStream completed",
		zap.Int("tool_calls", toolCallCount),
		zap.Int("total_content_len", totalContentLen),
		zap.Duration("duration", time.Since(startTime)),
	)
	return nil
}

func (a *DiagnosisAgent) RunStreamWithMessages(ctx context.Context, messages []*schema.Message, systemPrompt string, callback func(chunk string, toolCall *ToolCallInfo) error) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	a.logger.Info(
		"DiagnosisAgent RunStreamWithMessages started",
		zap.Int("history_msgs", len(messages)),
		zap.Int("tools", len(a.tools)),
		zap.Int("max_steps", a.maxSteps),
	)

	var baseTools []tool.BaseTool
	for _, t := range a.tools {
		baseTools = append(baseTools, t)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "diagnosis_agent",
		Description:   "Kubernetes AI diagnosis agent",
		Instruction:   systemPrompt,
		Model:         a.chatModel,
		MaxIterations: a.maxSteps,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: baseTools,
			},
		},
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  3,
			IsRetryAble: func(ctx context.Context, err error) bool { return err != nil },
			BackoffFunc: func(ctx context.Context, attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second },
		},
		Handlers: []adk.ChatModelAgentMiddleware{
			&safeToolHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}},
		},
	})
	if err != nil {
		return fmt.Errorf("create diagnosis agent: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})

	cbHelper := cbt.NewHandlerHelper().
		Agent(&cbt.AgentCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *adk.AgentCallbackInput) context.Context {
				a.logger.Debug("Agent callback start", zap.String("agent", info.Name))
				return ctx
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *adk.AgentCallbackOutput) context.Context {
				a.logger.Debug("Agent callback end", zap.String("agent", info.Name))
				return ctx
			},
		}).
		Handler()

	startTime := time.Now()
	toolCallCount := 0
	totalContentLen := 0
	eventCount := 0
	iter := runner.Run(ctx, messages, adk.WithCallbacks(cbHelper))

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		eventCount++

		if event.Err != nil {
			a.logger.Warn(
				"Agent event error, continuing",
				zap.Error(event.Err),
				zap.Int("event_num", eventCount),
			)
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msgOut := event.Output.MessageOutput

			if msgOut.IsStreaming && msgOut.MessageStream != nil {
				stream := msgOut.MessageStream
				chunkCount := 0
				for {
					chunk, err := stream.Recv()
					if err != nil {
						if err != io.EOF {
							a.logger.Warn(
								"Stream recv error",
								zap.Error(err),
								zap.Int("chunks_before_error", chunkCount),
								zap.Int("event_num", eventCount),
							)
						}
						break
					}
					if chunk.Content != "" {
						chunkCount++
						totalContentLen += len(chunk.Content)
						if err := callback(chunk.Content, nil); err != nil {
							return fmt.Errorf("callback error: %w", err)
						}
					}
					for _, tc := range chunk.ToolCalls {
						if tc.Function.Name == "" {
							continue
						}
						toolCallCount++
						a.logger.Info(
							"Agent stream tool call",
							zap.Int("call_num", toolCallCount),
							zap.String("tool", tc.Function.Name),
						)
						if err := callback("", &ToolCallInfo{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						}); err != nil {
							return fmt.Errorf("callback error: %w", err)
						}
					}
				}
			} else if !msgOut.IsStreaming && msgOut.Message != nil && msgOut.Message.Content != "" {
				totalContentLen += len(msgOut.Message.Content)
				if err := callback(msgOut.Message.Content, nil); err != nil {
					return fmt.Errorf("callback error: %w", err)
				}
			}
		}
	}

	a.logger.Info(
		"DiagnosisAgent RunStreamWithMessages completed",
		zap.Int("tool_calls", toolCallCount),
		zap.Int("total_content_len", totalContentLen),
		zap.Int("events_processed", eventCount),
		zap.Duration("duration", time.Since(startTime)),
	)
	return nil
}

// safeToolHandler 包装工具调用，将执行错误转为可读文本返回给 LLM，让 Agent 自行修正而非中断。
type safeToolHandler struct {
	*adk.BaseChatModelAgentMiddleware
}

func (h *safeToolHandler) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return fmt.Sprintf("[Tool Error] %v. Please try a different approach or use a different tool.", err), nil
		}
		return result, nil
	}, nil
}
