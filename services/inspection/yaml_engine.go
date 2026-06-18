package inspection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
)

// Rule 巡检规则定义
type Rule struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Query       string      `yaml:"query"`
	Check       CheckConfig `yaml:"check"`
	Severity    string      `yaml:"severity"`
	Suggestion  string      `yaml:"suggestion"`
}

// CheckConfig 检查配置
type CheckConfig struct {
	Type      string                 `yaml:"type"`
	Threshold int                    `yaml:"threshold,omitempty"`
	Field     string                 `yaml:"field,omitempty"`
	Value     string                 `yaml:"value,omitempty"`
	Operator  string                 `yaml:"operator,omitempty"`
	PromQL    string                 `yaml:"promQL,omitempty"`
	Command   string                 `yaml:"command,omitempty"`
	Timeout   string                 `yaml:"timeout,omitempty"`
	Params    map[string]interface{} `yaml:"params,omitempty"`
}

// Finding 检查发现
type Finding struct {
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

// PluginInput 传给外部命令插件的输入
type PluginInput struct {
	Rule   Rule                     `json:"rule"`
	Rows   []map[string]interface{} `json:"rows"`
	Params map[string]interface{}   `json:"params"`
}

// PluginOutput 外部命令插件的输出
type PluginOutput struct {
	Findings []Finding `json:"findings"`
}

// RuleResult 单条规则的执行结果
type RuleResult struct {
	Name     string    `json:"name"`
	Findings []Finding `json:"findings"`
	Severity string    `json:"severity"`
}

// Report 巡检报告
type Report struct {
	Timestamp time.Time    `json:"timestamp"`
	Rules     []RuleResult `json:"rules"`
}

// YAMLEngine 声明式巡检引擎
type YAMLEngine struct {
	logger  interfaces.Logger
	graphDB interfaces.GraphDB
	rules   []Rule
}

func NewYAMLEngine(logger interfaces.Logger, graphDB interfaces.GraphDB, rules []Rule) *YAMLEngine {
	return &YAMLEngine{logger: logger, graphDB: graphDB, rules: rules}
}

func (e *YAMLEngine) Execute(ctx context.Context) *Report {
	report := &Report{Timestamp: time.Now()}
	e.logger.Info("YAML inspection engine started", zap.Int("ruleCount", len(e.rules)))

	for _, rule := range e.rules {
		e.logger.Debug("executing inspection rule",
			zap.String("rule", rule.Name),
			zap.String("type", rule.Check.Type))
		rows, err := e.executeQuery(rule.Query)
		if err != nil {
			e.logger.Warn("inspection rule query failed",
				zap.String("rule", rule.Name), zap.Error(err))
			continue
		}
		e.logger.Debug("inspection query completed",
			zap.String("rule", rule.Name), zap.Int("rows", len(rows)))

		findings, err := e.check(rule, rows)
		if err != nil {
			e.logger.Warn("inspection rule check failed",
				zap.String("rule", rule.Name), zap.Error(err))
			continue
		}
		report.Rules = append(report.Rules, RuleResult{
			Name: rule.Name, Findings: findings, Severity: rule.Severity,
		})
		e.logger.Info("inspection rule completed",
			zap.String("rule", rule.Name),
			zap.Int("findings", len(findings)),
			zap.String("severity", rule.Severity))
	}
	return report
}

func (e *YAMLEngine) executeQuery(query string) ([]map[string]interface{}, error) {
	rs, err := e.graphDB.ExecuteAndCheck(query)
	if err != nil {
		return nil, err
	}
	if rs == nil {
		return nil, nil
	}
	var rows []map[string]interface{}
	for i := 0; i < rs.GetRowSize(); i++ {
		row, err := rs.GetRowValuesByIndex(i)
		if err != nil {
			continue
		}
		m := make(map[string]interface{})
		for _, col := range rs.GetColNames() {
			v, err := row.GetValueByColName(col)
			if err != nil {
				continue
			}
			if v.IsString() {
				s, _ := v.AsString()
				m[col] = s
			} else if v.IsInt() {
				n, _ := v.AsInt()
				m[col] = n
			} else if v.IsFloat() {
				f, _ := v.AsFloat()
				m[col] = f
			} else if v.IsBool() {
				b, _ := v.AsBool()
				m[col] = b
			}
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// CheckRule 对单条规则执行查询和检查，返回检查发现
func (e *YAMLEngine) CheckRule(rule Rule) ([]Finding, error) {
	rows, err := e.executeQuery(rule.Query)
	if err != nil {
		return nil, err
	}
	return e.check(rule, rows)
}

func (e *YAMLEngine) check(rule Rule, rows []map[string]interface{}) ([]Finding, error) {
	switch rule.Check.Type {
	case "min_rows":
		return checkMinRows(rule, rows), nil
	case "field_contains":
		return checkFieldContains(rule, rows), nil
	case "command":
		findings, err := checkCommand(rule, rows)
		return findings, err
	default:
		return nil, fmt.Errorf("unknown check type: %s", rule.Check.Type)
	}
}

func checkMinRows(rule Rule, rows []map[string]interface{}) []Finding {
	if len(rows) < rule.Check.Threshold {
		return nil
	}
	var findings []Finding
	for _, row := range rows {
		name := fmt.Sprintf("%v", row["name"])
		ns := fmt.Sprintf("%v", row["namespace"])
		loc := name
		if ns != "" && ns != "<nil>" {
			loc = ns + "/" + name
		}
		findings = append(findings, Finding{
			Resource:  loc,
			Namespace: ns,
			Severity:  rule.Severity,
			Message:   fmt.Sprintf("%s: %s", loc, rule.Suggestion),
		})
	}
	return findings
}

func checkFieldContains(rule Rule, rows []map[string]interface{}) []Finding {
	var findings []Finding
	for _, row := range rows {
		val, _ := row[rule.Check.Field].(string)
		if contains(val, rule.Check.Value) {
			name := fmt.Sprintf("%v", row["name"])
			ns := fmt.Sprintf("%v", row["namespace"])
			loc := name
			if ns != "" && ns != "<nil>" {
				loc = ns + "/" + name
			}
			findings = append(findings, Finding{
				Resource:  loc,
				Namespace: ns,
				Severity:  rule.Severity,
				Message:   fmt.Sprintf("%s: %s", loc, rule.Suggestion),
			})
		}
	}
	return findings
}

func checkCommand(rule Rule, rows []map[string]interface{}) ([]Finding, error) {
	input := PluginInput{Rule: rule, Rows: rows, Params: rule.Check.Params}
	stdin, _ := json.Marshal(input)
	timeout := 30 * time.Second
	if rule.Check.Timeout != "" {
		if d, err := time.ParseDuration(rule.Check.Timeout); err == nil {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", rule.Check.Command) //nolint:gosec
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("plugin %s failed: %w, stderr: %s", rule.Name, err, stderr.String())
	}
	var output PluginOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("plugin %s output parse failed: %w", rule.Name, err)
	}
	return output.Findings, nil
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && bytes.Contains([]byte(s), []byte(substr))
}
