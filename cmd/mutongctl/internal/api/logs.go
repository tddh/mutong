package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// LogsAPI provides typed access to the Mutong Logs API.
type LogsAPI struct {
	client *client.Client
}

// NewLogsAPI creates a new LogsAPI.
func NewLogsAPI(c *client.Client) *LogsAPI {
	return &LogsAPI{client: c}
}

// LogEntry represents a single log entry from the Mutong API.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Message   string `json:"message"`
	Level     string `json:"level"`
}

// GetPodLogs retrieves pod logs, optionally filtered by namespace and tail limit.
func (a *LogsAPI) GetPodLogs(name, namespace string, tail int) ([]LogEntry, error) {
	path := "/api/v1/logs/pod?name=" + name
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	if tail > 0 {
		path += "&tail=" + strconv.Itoa(tail)
	}
	return a.fetchLogs(path)
}

// SearchLogs searches logs by keyword, optionally filtered by namespace and time range.
func (a *LogsAPI) SearchLogs(keyword, namespace, since string) ([]LogEntry, error) {
	path := "/api/v1/logs/search?q=" + keyword
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	if since != "" {
		path += "&since=" + since
	}
	return a.fetchLogs(path)
}

// GetErrorLogs retrieves error-level logs, optionally filtered by namespace and time range.
func (a *LogsAPI) GetErrorLogs(namespace, since string) ([]LogEntry, error) {
	path := "/api/v1/logs/errors"
	var params []string
	if namespace != "" {
		params = append(params, "namespace="+namespace)
	}
	if since != "" {
		params = append(params, "since="+since)
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	return a.fetchLogs(path)
}

func (a *LogsAPI) fetchLogs(path string) ([]LogEntry, error) {
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching logs: %w", err)
	}
	var entries []LogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
