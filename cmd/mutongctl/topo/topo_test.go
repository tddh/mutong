package topo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestTopoGetRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"}],"edges":[{"source":"uid-1","target":"uid-2","label":"BelongsTo"}],"totalCount":2}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &TopoGetOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		UID:    "uid-1",
		Depth:  2,
		Format: "json",
	}
	err := topoGetRun(opts)
	if err != nil {
		t.Fatalf("topoGetRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "uid-1") {
		t.Errorf("output should contain 'uid-1', got: %s", stdout.String())
	}
}

func TestTopoGetRunURL(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[],"edges":[],"totalCount":0}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &TopoGetOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		UID:    "uid-abc",
		Depth:  3,
		Format: "json",
	}
	err := topoGetRun(opts)
	if err != nil {
		t.Fatalf("topoGetRun error: %v", err)
	}
	expected := "/k8s/resources/graph/search?resourceId=uid-abc&depth=3"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestTopoSearchRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &TopoSearchOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Kind:   "Pod",
		Name:   "nginx",
		Format: "json",
	}
	err := topoSearchRun(opts)
	if err != nil {
		t.Fatalf("topoSearchRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "nginx") {
		t.Errorf("output should contain 'nginx', got: %s", stdout.String())
	}
}

func TestTopoSearchRunNoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"nodes":[{"id":"uid-1","label":"nginx","kind":"Pod","namespace":"default"}],"edges":[],"totalCount":1}`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &TopoSearchOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Kind:   "Pod",
		Format: "json",
	}
	err := topoSearchRun(opts)
	if err != nil {
		t.Fatalf("topoSearchRun error: %v", err)
	}
}

func TestNewCmdTopoGet(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdTopoGet(f, nil)
	if cmd.Use != "get <uid>" {
		t.Errorf("expected Use 'get <uid>', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("depth") == nil {
		t.Fatal("expected --depth flag")
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdTopoSearch(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdTopoSearch(f, nil)
	if cmd.Use != "search" {
		t.Errorf("expected Use 'search', got '%s'", cmd.Use)
	}
	if cmd.Flags().Lookup("kind") == nil {
		t.Fatal("expected --kind flag")
	}
	if cmd.Flags().Lookup("name") == nil {
		t.Fatal("expected --name flag")
	}
	if cmd.Flags().Lookup("output") == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdTopo(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdTopo(f)
	if cmd.Use != "topo" {
		t.Errorf("expected Use 'topo', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
