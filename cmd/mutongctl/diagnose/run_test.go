package diagnose

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestRunDiagnosisRun(t *testing.T) {
	var reqMethod, reqPath string
	var reqBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMethod = r.Method
		reqPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"diag-001","timestamp":"2026-01-01T00:00:00Z","summary":"CPU throttling detected on pod nginx-abc"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &RunDiagnosisOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "fp-123",
		Format:      "json",
	}
	err := runDiagnosisRun(opts)
	if err != nil {
		t.Fatalf("runDiagnosisRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "diag-001") {
		t.Errorf("output should contain 'diag-001', got: %s", stdout.String())
	}
	if reqMethod != "POST" {
		t.Errorf("expected POST, got %s", reqMethod)
	}
	if reqPath != "/api/v1/diagnosis/run" {
		t.Errorf("expected /api/v1/diagnosis/run, got %s", reqPath)
	}
	_ = reqBody
}

func TestDiagnosisStatusRun(t *testing.T) {
	var reqPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"llm":{"configured":true},"diagnosis":{"totalDiagnoses":10}}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &DiagnosisStatusOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := diagnosisStatusRun(opts)
	if err != nil {
		t.Fatalf("diagnosisStatusRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "totalDiagnoses") {
		t.Errorf("output should contain 'totalDiagnoses', got: %s", stdout.String())
	}
	if reqPath != "/api/v1/diagnosis/status" {
		t.Errorf("expected /api/v1/diagnosis/status, got %s", reqPath)
	}
}

func TestNewCmdRunDiagnosis(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdRunDiagnosis(f, nil)
	if cmd.Use != "run" {
		t.Errorf("expected Use 'run', got '%s'", cmd.Use)
	}
	fpFlag := cmd.Flags().Lookup("fingerprint")
	if fpFlag == nil {
		t.Fatal("expected --fingerprint flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdDiagnosisStatus(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdDiagnosisStatus(f, nil)
	if cmd.Use != "status" {
		t.Errorf("expected Use 'status', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdDiagnose(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdDiagnose(f)
	if cmd.Use != "diagnose" {
		t.Errorf("expected Use 'diagnose', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}

func TestRunDiagnosisRunError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &RunDiagnosisOptions{
		IO:          ios,
		Client:      client.NewClient(srv.URL, "token", "test"),
		Fingerprint: "fp-err",
		Format:      "json",
	}
	err := runDiagnosisRun(opts)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
