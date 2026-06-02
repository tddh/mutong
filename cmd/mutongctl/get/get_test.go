package get

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestGetPodRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default","status":"Running"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetPodOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Name:      "nginx",
		Namespace: "default",
		Format:    "json",
	}
	err := getPodRun(opts)
	if err != nil {
		t.Fatalf("getPodRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "nginx") {
		t.Errorf("output should contain 'nginx', got: %s", stdout.String())
	}
}

func TestGetPodRunNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"redis","kind":"Pod","namespace":"default","status":"Running"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &GetPodOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Name:      "nginx",
		Namespace: "default",
		Format:    "json",
	}
	err := getPodRun(opts)
	if err == nil {
		t.Fatal("expected error for missing pod, got nil")
	}
}

func TestGetNodeRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"nodes":[{"id":"node-1","label":"worker-1","kind":"Node","status":"Ready"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &GetNodeOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Name:   "worker-1",
		Format: "json",
	}
	err := getNodeRun(opts)
	if err != nil {
		t.Fatalf("getNodeRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "worker-1") {
		t.Errorf("output should contain 'worker-1', got: %s", stdout.String())
	}
}

func TestGetNodeRunNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"nodes":[],"edges":[],"totalCount":0}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &GetNodeOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Name:   "nonexistent",
		Format: "json",
	}
	err := getNodeRun(opts)
	if err == nil {
		t.Fatal("expected error for missing node, got nil")
	}
}

func TestGetPodRunURL(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"kube-system","status":"Running"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &GetPodOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Name:      "nginx",
		Namespace: "kube-system",
		Format:    "json",
	}
	err := getPodRun(opts)
	if err != nil {
		t.Fatalf("getPodRun error: %v", err)
	}
	expected := "/k8s/resources/graph/nodes?kind=Pod&namespace=kube-system"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestNewCmdGetPod(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetPod(f, nil)
	if cmd.Use != "pod <name>" {
		t.Errorf("expected Use 'pod <name>', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("namespace") == nil {
		t.Fatal("expected --namespace flag")
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGetNode(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGetNode(f, nil)
	if cmd.Use != "node <name>" {
		t.Errorf("expected Use 'node <name>', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdGet(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdGet(f)
	if cmd.Use != "get" {
		t.Errorf("expected Use 'get', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
