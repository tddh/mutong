package logs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestPodLogsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"timestamp":"2026-01-01T00:00:00Z","pod":"nginx","container":"nginx","message":"Server started","level":"info"},
			{"timestamp":"2026-01-01T00:01:00Z","pod":"nginx","container":"nginx","message":"Request served","level":"info"}
		]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &PodLogsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Pod:    "nginx",
		Format: "json",
	}
	err := podLogsRun(opts)
	if err != nil {
		t.Fatalf("podLogsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Server started") {
		t.Errorf("output should contain 'Server started', got: %s", stdout.String())
	}
}

func TestPodLogsRunWithTail(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &PodLogsOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Pod:       "nginx",
		Namespace: "default",
		Tail:      100,
		Format:    "json",
	}
	err := podLogsRun(opts)
	if err != nil {
		t.Fatalf("podLogsRun error: %v", err)
	}
	expected := "/api/v1/logs/pod?name=nginx&namespace=default&tail=100"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestSearchLogsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"timestamp":"2026-01-01T00:00:00Z","pod":"api","container":"api","message":"error connecting to database","level":"error"}
		]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &SearchLogsOptions{
		IO:      ios,
		Client:  client.NewClient(srv.URL, "token", "test"),
		Keyword: "database",
		Format:  "json",
	}
	err := searchLogsRun(opts)
	if err != nil {
		t.Fatalf("searchLogsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "connecting to database") {
		t.Errorf("output should contain 'connecting to database', got: %s", stdout.String())
	}
}

func TestSearchLogsRunWithFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &SearchLogsOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Keyword:   "timeout",
		Namespace: "prod",
		Since:     "1h",
		Format:    "json",
	}
	err := searchLogsRun(opts)
	if err != nil {
		t.Fatalf("searchLogsRun error: %v", err)
	}
	expected := "/api/v1/logs/search?q=timeout&namespace=prod&since=1h"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestErrorLogsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"timestamp":"2026-01-01T00:00:00Z","pod":"worker","container":"worker","message":"OOMKilled","level":"error"}
		]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ErrorLogsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := errorLogsRun(opts)
	if err != nil {
		t.Fatalf("errorLogsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "OOMKilled") {
		t.Errorf("output should contain 'OOMKilled', got: %s", stdout.String())
	}
}

func TestErrorLogsRunWithFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ErrorLogsOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Namespace: "staging",
		Since:     "30m",
		Format:    "json",
	}
	err := errorLogsRun(opts)
	if err != nil {
		t.Fatalf("errorLogsRun error: %v", err)
	}
	expected := "/api/v1/logs/errors?namespace=staging&since=30m"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestErrorLogsRunNoFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ErrorLogsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := errorLogsRun(opts)
	if err != nil {
		t.Fatalf("errorLogsRun error: %v", err)
	}
	expected := "/api/v1/logs/errors"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestNewCmdLogsPod(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdLogsPod(f, nil)
	if cmd.Use != "pod <name>" {
		t.Errorf("expected Use 'pod <name>', got '%s'", cmd.Use)
	}
	namespaceFlag := cmd.Flags().Lookup("namespace")
	if namespaceFlag == nil {
		t.Fatal("expected --namespace flag")
	}
	tailFlag := cmd.Flags().Lookup("tail")
	if tailFlag == nil {
		t.Fatal("expected --tail flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdLogsSearch(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdLogsSearch(f, nil)
	if cmd.Use != "search <keyword>" {
		t.Errorf("expected Use 'search <keyword>', got '%s'", cmd.Use)
	}
	sinceFlag := cmd.Flags().Lookup("since")
	if sinceFlag == nil {
		t.Fatal("expected --since flag")
	}
}

func TestNewCmdLogsErrors(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdLogsErrors(f, nil)
	if cmd.Use != "errors" {
		t.Errorf("expected Use 'errors', got '%s'", cmd.Use)
	}
	sinceFlag := cmd.Flags().Lookup("since")
	if sinceFlag == nil {
		t.Fatal("expected --since flag")
	}
}

func TestNewCmdLogs(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdLogs(f)
	if cmd.Use != "logs" {
		t.Errorf("expected Use 'logs', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 3 {
		t.Errorf("expected 3 subcommands, got %d", len(cmd.Commands()))
	}
}
