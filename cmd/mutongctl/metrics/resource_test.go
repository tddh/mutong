package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

func TestResourceRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"metricName":"pod_cpu_usage_cores","value":0.5,"labels":{"pod":"test-pod"},"status":"normal"}]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &ResourceOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		Format:    "json",
	}
	err := resourceRun(opts)
	if err != nil {
		t.Fatalf("resourceRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "pod_cpu_usage_cores") {
		t.Errorf("output should contain 'pod_cpu_usage_cores', got: %s", stdout.String())
	}
}

func TestResourceRunURL(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ResourceOptions{
		IO:        ios,
		Client:    client.NewClient(srv.URL, "token", "test"),
		Kind:      "Pod",
		Name:      "my-pod",
		Namespace: "default",
		Format:    "json",
	}
	err := resourceRun(opts)
	if err != nil {
		t.Fatalf("resourceRun error: %v", err)
	}
	expected := "/api/v1/monitoring/metrics/Pod?name=my-pod&namespace=default"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestResourceRunNoNamespace(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &ResourceOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Kind:   "Node",
		Name:   "node-1",
		Format: "json",
	}
	err := resourceRun(opts)
	if err != nil {
		t.Fatalf("resourceRun error: %v", err)
	}
	expected := "/api/v1/monitoring/metrics/Node?name=node-1"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestTimeseriesRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"metricName":"cpu","values":[{"timestamp":1700000000,"value":0.5}],"labels":{}}]`))
	}))
	defer srv.Close()

	ios, stdout, _ := iostreams.Test()
	opts := &TimeseriesOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Expr:   "rate(cpu[5m])",
		Start:  "1700000000",
		End:    "1700003600",
		Step:   "60",
		Format: "json",
	}
	err := timeseriesRun(opts)
	if err != nil {
		t.Fatalf("timeseriesRun error: %v", err)
	}
	if !strings.Contains(stdout.String(), "cpu") {
		t.Errorf("output should contain 'cpu', got: %s", stdout.String())
	}
}

func TestTimeseriesRunURL(t *testing.T) {
	var reqURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	ios, _, _ := iostreams.Test()
	opts := &TimeseriesOptions{
		IO:     ios,
		Client: client.NewClient(srv.URL, "token", "test"),
		Expr:   "rate(cpu[5m])",
		Start:  "1700000000",
		End:    "1700003600",
		Step:   "60",
		Format: "json",
	}
	err := timeseriesRun(opts)
	if err != nil {
		t.Fatalf("timeseriesRun error: %v", err)
	}
	expected := "/api/v1/monitoring/timeseries?expr=rate(cpu[5m])&start=1700000000&end=1700003600&step=60"
	if reqURL != expected {
		t.Errorf("expected URL %q, got %q", expected, reqURL)
	}
}

func TestNewCmdResource(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdResource(f, nil)
	if cmd.Use != "resource <kind>" {
		t.Errorf("expected Use 'resource <kind>', got '%s'", cmd.Use)
	}
	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Fatal("expected --name flag")
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

func TestNewCmdTimeseries(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdTimeseries(f, nil)
	if cmd.Use != "timeseries" {
		t.Errorf("expected Use 'timeseries', got '%s'", cmd.Use)
	}
	exprFlag := cmd.Flags().Lookup("expr")
	if exprFlag == nil {
		t.Fatal("expected --expr flag")
	}
	startFlag := cmd.Flags().Lookup("start")
	if startFlag == nil {
		t.Fatal("expected --start flag")
	}
	endFlag := cmd.Flags().Lookup("end")
	if endFlag == nil {
		t.Fatal("expected --end flag")
	}
	stepFlag := cmd.Flags().Lookup("step")
	if stepFlag == nil {
		t.Fatal("expected --step flag")
	}
	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag")
	}
}

func TestNewCmdMetrics(t *testing.T) {
	f := &Factory{
		IO:     &iostreams.IOStreams{},
		Client: client.NewClient("http://localhost", "", ""),
	}
	cmd := NewCmdMetrics(f)
	if cmd.Use != "metrics" {
		t.Errorf("expected Use 'metrics', got '%s'", cmd.Use)
	}
	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
