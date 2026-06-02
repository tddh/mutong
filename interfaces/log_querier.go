package interfaces

import "context"

// LogQuerier 提供日志查询能力
type LogQuerier interface {
	GetPodLogs(ctx context.Context, namespace, podName, container string, sinceMinutes int, tail int) ([]LogEntry, error)
	SearchLogs(ctx context.Context, query, namespace string, sinceMinutes int, maxResults int) ([]LogEntry, error)
	GetErrorLogs(ctx context.Context, namespace string, sinceMinutes int) ([]LogEntry, error)
	GetWarnLogs(ctx context.Context, namespace string, sinceMinutes int) ([]LogEntry, error)
	GetInfoLogs(ctx context.Context, namespace string, sinceMinutes int) ([]LogEntry, error)
}

type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Pod       string            `json:"pod"`
	Namespace string            `json:"namespace"`
	Container string            `json:"container"`
	Labels    map[string]string `json:"labels,omitempty"`
}
