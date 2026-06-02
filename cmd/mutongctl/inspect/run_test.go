package inspect

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestRunInspectRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/inspection/execute" {
			t.Errorf("expected /api/v1/inspection/execute, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"rpt-001","message":"inspection completed","timestamp":"2026-01-01T00:00:00Z","status":"passed"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &RunInspectOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := runInspectRun(opts)
	if err != nil {
		t.Fatalf("runInspectRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "rpt-001") {
		t.Errorf("output should contain 'rpt-001', got: %s", stdout.String())
	}
}

func TestListInspectsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"rpt-001","timestamp":"2026-01-01T00:00:00Z","status":"passed","pass_count":10,"fail_count":0,"warn_count":2,"total_rules":12,"duration":"15s"}]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ListInspectsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listInspectsRun(opts)
	if err != nil {
		t.Fatalf("listInspectsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "rpt-001") {
		t.Errorf("output should contain 'rpt-001', got: %s", stdout.String())
	}
}

func TestGetInspectRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/inspection/reports/rpt-001" {
			t.Errorf("expected path /api/v1/inspection/reports/rpt-001, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"rpt-001","timestamp":"2026-01-01T00:00:00Z","status":"passed","pass_count":10,"fail_count":0,"warn_count":2,"total_rules":12,"duration":"15s"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetInspectOptions{
		IO:       ios,
		Client:   client.NewClient(srv.URL, "token", "test"),
		ReportID: "rpt-001",
		Format:   "json",
	}
	err := getInspectRun(opts)
	if err != nil {
		t.Fatalf("getInspectRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "rpt-001") {
		t.Errorf("output should contain 'rpt-001', got: %s", stdout.String())
	}
}

func TestCompareInspectsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/api/v1/inspection/reports/rpt-001/compare/rpt-002"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"newIssues":[{"ruleName":"rpt-001-issue","severity":"warning","message":"new issue","resources":[],"suggestion":"fix","timestamp":"2026-01-01T00:00:00Z"}],"resolvedIssues":[],"persistentIssues":[],"trend":"stable"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &CompareInspectsOptions{
		IO:      ios,
		Client:  client.NewClient(srv.URL, "token", "test"),
		Report1: "rpt-001",
		Report2: "rpt-002",
		Format:  "json",
	}
	err := compareInspectsRun(opts)
	if err != nil {
		t.Fatalf("compareInspectsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "rpt-001") {
		t.Errorf("output should contain 'rpt-001', got: %s", stdout.String())
	}
}

func TestInspectTrendRun(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"days":30,"trend":[{"date":"2026-01-01","pass_rate":0.95,"fail_count":0,"warn_count":1,"total":12}]}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &InspectTrendOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Days:   30,
		Format: "json",
	}
	err := inspectTrendRun(opts)
	if err != nil {
		t.Fatalf("inspectTrendRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "2026-01-01") {
		t.Errorf("output should contain '2026-01-01', got: %s", stdout.String())
	}
	expected := "/api/v1/inspection/trend?days=30"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestInspectTrendRunDefaultDays(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"days":7,"trend":[]}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &InspectTrendOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Days:   7,
		Format: "json",
	}
	err := inspectTrendRun(opts)
	if err != nil {
		t.Fatalf("inspectTrendRun error: %v", err)
	}
	expected := "/api/v1/inspection/trend?days=7"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestNewCmdRunInspect(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdRunInspect(f, nil)
	if cmd.Use != "run" {
		t.Errorf("expected Use 'run', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGetInspect(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetInspect(f, nil)
	if cmd.Use != "get <id>" {
		t.Errorf("expected Use 'get <id>', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdCompareInspects(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdCompareInspects(f, nil)
	if cmd.Use != "compare <id1> <id2>" {
		t.Errorf("expected Use 'compare <id1> <id2>', got '%s'", cmd.Use)
	}
}

func TestNewCmdInspectTrend(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdInspectTrend(f, nil)
	if cmd.Use != "trend" {
		t.Errorf("expected Use 'trend', got '%s'", cmd.Use)
	}
	daysFlag := cmd.Flags().Lookup("days")
	if daysFlag == nil {
		t.Fatal("expected --days flag")
	}
}

func TestNewCmdInspect(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdInspect(f)
	if cmd.Use != "inspect" {
		t.Errorf("expected Use 'inspect', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 6 {
		t.Errorf("expected 6 subcommands, got %d", len(cmd.Commands()))
	}
}
