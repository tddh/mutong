package stats

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestGetOverviewRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"total_namespaces":10,"total_pods":150,"total_deployments":45,"total_services":30,"total_nodes":8,"cluster_count":2}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetOverviewOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := getOverviewRun(opts)
	if err != nil {
		t.Fatalf("getOverviewRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "total_pods") {
		t.Errorf("output should contain 'total_pods', got: %s", stdout.String())
	}
}

func TestSyncStatsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","message":"sync completed","synced":150}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &SyncStatsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := syncStatsRun(opts)
	if err != nil {
		t.Fatalf("syncStatsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "sync completed") {
		t.Errorf("output should contain 'sync completed', got: %s", stdout.String())
	}
}

func TestSyncStatsRunError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &SyncStatsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := syncStatsRun(opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewCmdGetOverview(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetOverview(f, nil)
	if cmd.Use != "overview" {
		t.Errorf("expected Use 'overview', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdSyncStats(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdSyncStats(f, nil)
	if cmd.Use != "sync" {
		t.Errorf("expected Use 'sync', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdStats(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdStats(f)
	if cmd.Use != "stats" {
		t.Errorf("expected Use 'stats', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
