package system

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestGetSystemStatusRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"1.0.0","uptime":"72h","components":{"nebula":"ok","postgres":"ok"}}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetSystemStatusOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := getSystemStatusRun(opts)
	if err != nil {
		t.Fatalf("getSystemStatusRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "1.0.0") {
		t.Errorf("output should contain '1.0.0', got: %s", stdout.String())
	}
}

func TestNewCmdGetSystemStatus(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetSystemStatus(f, nil)
	if cmd.Use != "status" {
		t.Errorf("expected Use 'status', got '%s'", cmd.Use)
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdSystem(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdSystem(f)
	if cmd.Use != "system" {
		t.Errorf("expected Use 'system', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 1 {
		t.Errorf("expected 1 subcommand, got %d", len(cmd.Commands()))
	}
}
