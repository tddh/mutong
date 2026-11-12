package controllers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// newFakeK8sAPI starts an httptest server speaking just enough of the K8s REST
// API for list/get calls, and returns a real clientset pointed at it.
func newFakeK8sAPI(t *testing.T) (*kubernetes.Clientset, *rest.Config) {
	t.Helper()
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/api/v1/namespaces", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"NamespaceList","apiVersion":"v1","metadata":{},"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"team-a"}}]}`)
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"PodList","apiVersion":"v1","metadata":{},"items":[{"metadata":{"name":"p1","namespace":"default"},"spec":{"containers":[{"name":"app"}],"initContainers":[{"name":"init"}]}}]}`)
	})
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"PodList","apiVersion":"v1","metadata":{},"items":[{"metadata":{"name":"p1","namespace":"default"},"spec":{"containers":[{"name":"app"}],"initContainers":[{"name":"init"}]}}]}`)
	})
	mux.HandleFunc("/api/v1/namespaces/default/pods/p1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"Pod","apiVersion":"v1","metadata":{"name":"p1","namespace":"default"},"spec":{"containers":[{"name":"app"},{"name":"sidecar"}],"initContainers":[{"name":"init"}]}}`)
	})
	mux.HandleFunc("/api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"NodeList","apiVersion":"v1","metadata":{},"items":[{"metadata":{"name":"n1"}}]}`)
	})
	mux.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"ServiceList","apiVersion":"v1","metadata":{},"items":[{"metadata":{"name":"s1","namespace":"default"}},{"metadata":{"name":"s2","namespace":"default"}}]}`)
	})
	mux.HandleFunc("/apis/apps/v1/deployments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"kind":"DeploymentList","apiVersion":"apps/v1","metadata":{},"items":[{"metadata":{"name":"d1","namespace":"default"}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := &rest.Config{Host: srv.URL}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client, cfg
}

// newFailingK8sAPI returns a clientset whose every request fails with 500.
func newFailingK8sAPI(t *testing.T) *kubernetes.Clientset {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"boom","code":500}`)
	}))
	t.Cleanup(srv.Close)
	client, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
