package alert

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestListAlertsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alerts":[{"fingerprint":"abc123","status":"firing","labels":{"alertname":"HighCPU","severity":"critical"},"namespace":"default","startsAt":"2026-01-01T00:00:00Z"}],"count":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ListAlertsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listAlertsRun(opts)
	if err != nil {
		t.Fatalf("listAlertsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "HighCPU") {
		t.Errorf("output should contain 'HighCPU', got: %s", stdout.String())
	}
}

func TestListAlertsRunWithFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alerts":[],"count":0}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ListAlertsOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Severity:  "critical",
		Namespace: "default",
		Format:    "json",
	}
	err := listAlertsRun(opts)
	if err != nil {
		t.Fatalf("listAlertsRun error: %v", err)
	}
	expected := "/api/v1/alerts/?severity=critical&namespace=default"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestListAlertsRunNoFilters(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alerts":[],"count":0}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ListAlertsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listAlertsRun(opts)
	if err != nil {
		t.Fatalf("listAlertsRun error: %v", err)
	}
	expected := "/api/v1/alerts/"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestGetAlertRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"fingerprint":"abc123","status":"firing","labels":{"alertname":"HighCPU","severity":"critical"}}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetAlertOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "abc123",
		Format:      "json",
	}
	err := getAlertRun(opts)
	if err != nil {
		t.Fatalf("getAlertRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "abc123") {
		t.Errorf("output should contain 'abc123', got: %s", stdout.String())
	}
}

func TestNewCmdListAlerts(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdListAlerts(f, nil)
	if cmd.Use != "list" {
		t.Errorf("expected Use 'list', got '%s'", cmd.Use)
	}
	severityFlag := cmd.Flags().Lookup("severity")
	if severityFlag == nil {
		t.Fatal("expected --severity flag")
	}
	namespaceFlag := cmd.Flags().Lookup("namespace")
	if namespaceFlag == nil {
		t.Fatal("expected --namespace flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGetAlert(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetAlert(f, nil)
	if cmd.Use != "get <fingerprint>" {
		t.Errorf("expected Use 'get <fingerprint>', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdAlert(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdAlert(f)
	if cmd.Use != "alert" {
		t.Errorf("expected Use 'alert', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 3 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
