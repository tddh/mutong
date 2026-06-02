package retro

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestGenerateRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"fingerprint":"abc123","title":"High CPU Incident","severity":"critical","status":"generated","root_cause":"CPU throttling","business_impact":"API degraded","resolution":"Scale up replicas","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GenerateOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "abc123",
		Format:      "json",
	}
	err := generateRun(opts)
	if err != nil {
		t.Fatalf("generateRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "High CPU Incident") {
		t.Errorf("output should contain 'High CPU Incident', got: %s", stdout.String())
	}
}

func TestListRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"fingerprint":"abc123","incident_title":"High CPU Incident","severity":"critical","start_time":"","end_time":"","duration":"","resource_kind":"","resource_name":"","namespace":"","root_cause_summary":"","created_at":"2026-01-01T00:00:00Z","updated_at":""}],"total":1,"page":1,"page_size":20}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ListOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listRun(opts)
	if err != nil {
		t.Fatalf("listRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "High CPU Incident") {
		t.Errorf("output should contain 'High CPU Incident', got: %s", stdout.String())
	}
}

func TestListRunWithFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[],"total":0,"page":2,"page_size":20}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ListOptions{
		IO:       ios,
		Client:   client.NewClient(srv.URL, "token", "test"),
		Severity: "critical",
		Page:     2,
		Format:   "json",
	}
	err := listRun(opts)
	if err != nil {
		t.Fatalf("listRun error: %v", err)
	}
	expected := "/api/v1/retrospective/list?severity=critical&page=2"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestGetRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"fingerprint":"abc123","title":"High CPU Incident","severity":"critical","status":"generated","root_cause":"CPU throttling","business_impact":"API degraded","resolution":"Scale up replicas","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "abc123",
		Format:      "json",
	}
	err := getRun(opts)
	if err != nil {
		t.Fatalf("getRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "abc123") {
		t.Errorf("output should contain 'abc123', got: %s", stdout.String())
	}
}

func TestExportRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte("# Postmortem: High CPU Incident\n\n## Root Cause\nCPU throttling"))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ExportOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "abc123",
	}
	err := exportRun(opts)
	if err != nil {
		t.Fatalf("exportRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "# Postmortem") {
		t.Errorf("output should contain '# Postmortem', got: %s", stdout.String())
	}
}

func TestSearchRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"fingerprint":"abc123","title":"High CPU Incident","severity":"critical","similarity":0.95,"summary":"CPU-related outage"}],"query":"CPU outage"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &SearchOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Query:  "CPU outage",
		Format: "json",
	}
	err := searchRun(opts)
	if err != nil {
		t.Fatalf("searchRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "CPU outage") {
		t.Errorf("output should contain 'CPU outage', got: %s", stdout.String())
	}
}

func TestNewCmdGenerate(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGenerate(f, nil)
	if cmd.Use != "generate <fingerprint>" {
		t.Errorf("expected Use 'generate <fingerprint>', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdList(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdList(f, nil)
	if cmd.Use != "list" {
		t.Errorf("expected Use 'list', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("severity") == nil {
		t.Fatal("expected --severity flag")
	}
	if cmd.Flags().Lookup("page") == nil {
		t.Fatal("expected --page flag")
	}
}

func TestNewCmdGet(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGet(f, nil)
	if cmd.Use != "get <fingerprint>" {
		t.Errorf("expected Use 'get <fingerprint>', got '%s'", cmd.Use)
	}
}

func TestNewCmdExport(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdExport(f, nil)
	if cmd.Use != "export <fingerprint>" {
		t.Errorf("expected Use 'export <fingerprint>', got '%s'", cmd.Use)
	}
}

func TestNewCmdSearch(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdSearch(f, nil)
	if cmd.Use != "search <query>" {
		t.Errorf("expected Use 'search <query>', got '%s'", cmd.Use)
	}
}

func TestNewCmdRetro(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdRetro(f)
	if cmd.Use != "retro" {
		t.Errorf("expected Use 'retro', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 7 {
		t.Errorf("expected 5 subcommands, got %d", len(cmd.Commands()))
	}
}
