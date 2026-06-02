package diagnosis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

type ToolHandler func(ctx context.Context, args map[string]string) (string, error)

type MCPToolAdapter struct {
	name        string
	description string
	params      []diagnosis.ParamDef
	handler     ToolHandler
	logger      interfaces.Logger
}

func (a *MCPToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := convertParamsToParameterInfo(a.params)

	return &schema.ToolInfo{
		Name:        a.name,
		Desc:        a.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

func (a *MCPToolAdapter) InvokableRun(ctx context.Context, argsInJSON string, opts ...tool.Option) (string, error) {
	startTime := time.Now()
	var argsMap map[string]string
	if err := json.Unmarshal([]byte(argsInJSON), &argsMap); err != nil {
		argsMap = map[string]string{"raw": argsInJSON}
	}

	a.logger.Info(
		"MCP tool executing",
		zap.String("tool", a.name),
		zap.Any("args", argsMap),
	)

	result, err := a.handler(ctx, argsMap)
	duration := time.Since(startTime)
	if err != nil {
		a.logger.Error(
			"MCP tool execution failed",
			zap.String("tool", a.name),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return "", fmt.Errorf("%s: %w", a.name, err)
	}

	resultLen := len(result)
	summary := result
	if resultLen > 200 {
		summary = result[:200] + "..."
	}
	a.logger.Info(
		"MCP tool executed successfully",
		zap.String("tool", a.name),
		zap.Int("result_len", resultLen),
		zap.Duration("duration", duration),
		zap.String("summary", summary),
	)
	return result, nil
}

type ToolDefAndHandler struct {
	Description string
	Params      []diagnosis.ParamDef
	Handler     ToolHandler
}

func ConvertToEinoTools(tools map[string]ToolDefAndHandler, logger interfaces.Logger) []tool.InvokableTool {
	var einoTools []tool.InvokableTool
	for name, def := range tools {
		einoTools = append(einoTools, &MCPToolAdapter{
			name:        name,
			description: def.Description,
			params:      def.Params,
			handler:     def.Handler,
			logger:      logger,
		})
	}
	return einoTools
}

func convertParamsToParameterInfo(params []diagnosis.ParamDef) map[string]*schema.ParameterInfo {
	result := make(map[string]*schema.ParameterInfo, len(params))
	for _, p := range params {
		result[p.Name] = &schema.ParameterInfo{
			Type:     schema.String,
			Desc:     p.Description,
			Required: p.Required,
		}
	}
	return result
}
