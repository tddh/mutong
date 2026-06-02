package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestListClustersRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"clusters":[{"name":"prod-cluster","status":"healthy","nodes":5}],"count":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ListClustersOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listClustersRun(opts)
	if err != nil {
		t.Fatalf("listClustersRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "prod-cluster") {
		t.Errorf("output should contain 'prod-cluster', got: %s", stdout.String())
	}
}

func TestGetClusterHealthRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"prod-cluster","status":"healthy"}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetClusterHealthOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Name:   "prod-cluster",
		Format: "json",
	}
	err := getClusterHealthRun(opts)
	if err != nil {
		t.Fatalf("getClusterHealthRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "healthy") {
		t.Errorf("output should contain 'healthy', got: %s", stdout.String())
	}
}

func TestGetClusterStatsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster":"prod-cluster","stats":{"nodes":5,"pods":120}}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetClusterStatsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Name:   "prod-cluster",
		Format: "json",
	}
	err := getClusterStatsRun(opts)
	if err != nil {
		t.Fatalf("getClusterStatsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "prod-cluster") {
		t.Errorf("output should contain 'prod-cluster', got: %s", stdout.String())
	}
}

func TestNewCmdListClusters(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdListClusters(f, nil)
	if cmd.Use != "list" {
		t.Errorf("expected Use 'list', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGetClusterHealth(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetClusterHealth(f, nil)
	if cmd.Use != "health <name>" {
		t.Errorf("expected Use 'health <name>', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGetClusterStats(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetClusterStats(f, nil)
	if cmd.Use != "stats <name>" {
		t.Errorf("expected Use 'stats <name>', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdCluster(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdCluster(f)
	if cmd.Use != "cluster" {
		t.Errorf("expected Use 'cluster', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 3 {
		t.Errorf("expected 3 subcommands, got %d", len(cmd.Commands()))
	}
}
