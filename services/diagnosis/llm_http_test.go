package diagnosis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/diagnosis"
)

func TestHTTPLLMProvider_AnthropicCompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("expected x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": `{"root_cause":"Pod OOMKilled","confidence":0.9,"evidence":["memory limit exceeded"],"remediation":{"action":"Increase memory limit","description":"Pod exceeded memory limit","steps":["Edit deployment","Increase limits.memory"],"risk_level":"low","auto_fixable":false}}`},
			},
		})
	}))
	defer server.Close()

	provider := NewHTTPLLMProvider("anthropic", "claude-sonnet-4-20250514", "test-key", server.URL, 30)
	result, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{
		Alert: map[string]string{"alertname": "OOMKilled"},
		TopologySnapshot: &diagnosis.TopologySnapshot{
			Nodes: []diagnosis.TopologyNode{{UID: "uid-1", Kind: "Pod", Name: "test-pod", Namespace: "default"}},
			Edges: []diagnosis.TopologyEdge{{From: "uid-1", To: "uid-2", Type: "RunsOn"}},
		},
		ImpactAssessment: &diagnosis.ImpactAssessment{
			Severity:         "critical",
			BlastRadius:      3,
			UserFacingImpact: true,
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RootCause != "Pod OOMKilled" {
		t.Errorf("expected root cause 'Pod OOMKilled', got %q", result.RootCause)
	}
	if result.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", result.Confidence)
	}
	if len(result.Evidence) != 1 || result.Evidence[0] != "memory limit exceeded" {
		t.Errorf("unexpected evidence: %v", result.Evidence)
	}
}

func TestHTTPLLMProvider_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"root_cause":"Node disk pressure","confidence":0.85,"evidence":["disk usage > 90%"],"remediation":{"action":"Clean up disk","description":"Node running out of disk space","steps":["Remove unused images","Clean logs"],"risk_level":"medium","auto_fixable":false}}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	provider := NewHTTPLLMProvider("openai", "gpt-4", "test-key", server.URL, 30)
	result, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{
		Alert:            map[string]string{"alertname": "NodeDiskPressure"},
		TopologySnapshot: &diagnosis.TopologySnapshot{Nodes: []diagnosis.TopologyNode{}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RootCause != "Node disk pressure" {
		t.Errorf("expected 'Node disk pressure', got %q", result.RootCause)
	}
}

func TestHTTPLLMProvider_UnsupportedProvider(t *testing.T) {
	provider := NewHTTPLLMProvider("unknown", "model", "key", "", 30)
	_, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestHTTPLLMProvider_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer server.Close()

	provider := NewHTTPLLMProvider("anthropic", "claude-sonnet-4-20250514", "bad-key", server.URL, 30)
	_, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{})
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

func TestHTTPLLMProvider_EnvVarAPIKey(t *testing.T) {
	t.Setenv("TEST_API_KEY", "env-resolved-key")
	provider := NewHTTPLLMProvider("anthropic", "claude-sonnet-4-20250514", "$TEST_API_KEY", "", 30)
	if provider.apiKey != "env-resolved-key" {
		t.Errorf("expected env-resolved key, got %q", provider.apiKey)
	}
}

func TestHTTPLLMProvider_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"type":"text","text":"not valid json"}]}`))
	}))
	defer server.Close()

	provider := NewHTTPLLMProvider("anthropic", "claude-sonnet-4-20250514", "test-key", server.URL, 30)
	_, err := provider.Diagnose(context.Background(), interfaces.DiagnosisPrompt{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// CODE-03：工具调用参数解析失败必须报错，不能静默当成空参数
func TestParseToolCallArgs(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{"empty args", "", map[string]string{}, false},
		{"blank args", "   ", map[string]string{}, false},
		{"empty object", "{}", map[string]string{}, false},
		{"null", "null", map[string]string{}, false},
		{"string args", `{"namespace":"default","pod":"p1"}`, map[string]string{"namespace": "default", "pod": "p1"}, false},
		{"malformed", `{"namespace":`, nil, true},
		{"not an object", `["a"]`, nil, true},
		{"non string value", `{"replicas":3}`, nil, true},
	}
	for _, tc := range cases {
		got, err := parseToolCallArgs(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %v", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: arg %q = %q, want %q", tc.name, k, got[k], v)
			}
		}
	}
}

func newToolCallServer(t *testing.T, arguments string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id": "call-1",
								"function": map[string]string{
									"name":      "get_pod_metrics",
									"arguments": arguments,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
}

func TestDiagnoseChatWithToolsRejectsMalformedArgs(t *testing.T) {
	server := newToolCallServer(t, `{"namespace":`)
	defer server.Close()

	provider := NewHTTPLLMProvider("openai", "gpt-4", "test-key", server.URL, 30)
	resp, err := provider.DiagnoseChatWithTools(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected error for malformed tool arguments, got %+v", resp)
	}
	if resp != nil {
		t.Errorf("no response should be returned on parse failure, got %+v", resp)
	}
}

func TestDiagnoseChatWithToolsParsesArgs(t *testing.T) {
	server := newToolCallServer(t, `{"namespace":"default","pod":"p1"}`)
	defer server.Close()

	provider := NewHTTPLLMProvider("openai", "gpt-4", "test-key", server.URL, 30)
	resp, err := provider.DiagnoseChatWithTools(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Arguments["namespace"] != "default" || resp.ToolCalls[0].Arguments["pod"] != "p1" {
		t.Errorf("unexpected arguments: %v", resp.ToolCalls[0].Arguments)
	}
}

// 无参工具调用（arguments 为空串）仍然合法
func TestDiagnoseChatWithToolsEmptyArgs(t *testing.T) {
	server := newToolCallServer(t, "")
	defer server.Close()

	provider := NewHTTPLLMProvider("openai", "gpt-4", "test-key", server.URL, 30)
	resp, err := provider.DiagnoseChatWithTools(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("no-arg tool call must stay valid: %v", err)
	}
	if len(resp.ToolCalls) != 1 || len(resp.ToolCalls[0].Arguments) != 0 {
		t.Errorf("expected a single no-arg tool call, got %+v", resp.ToolCalls)
	}
}
