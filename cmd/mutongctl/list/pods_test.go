package list

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestListPodsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default","status":"Running"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ListPodsOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Namespace: "default",
		Format:    "json",
	}
	err := listPodsRun(opts)
	if err != nil {
		t.Fatalf("listPodsRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "nginx") {
		t.Errorf("output should contain 'nginx', got: %s", stdout.String())
	}
}

func TestListPodsRunNoNamespace(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Write([]byte(`{"nodes":[],"edges":[],"totalCount":0}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ListPodsOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Format: "json",
	}
	err := listPodsRun(opts)
	if err != nil {
		t.Fatalf("listPodsRun error: %v", err)
	}
	expected := "/k8s/resources/graph/nodes?kind=Pod"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestNewCmdListPods(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdListPods(f, nil)
	if cmd.Use != "pods" {
		t.Errorf("expected Use 'pods', got '%s'", cmd.Use)
	}
	nsFlag := cmd.Flags().Lookup("namespace")
	if nsFlag == nil {
		t.Fatal("expected --namespace flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}
