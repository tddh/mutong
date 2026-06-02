package api

import (
	"context"
	"encoding/json"
	"fmt"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
)

// ResourcesAPI provides typed access to the Mutong K8s Resources API.
type ResourcesAPI struct {
	client *client.Client
}

// NewResourcesAPI creates a new ResourcesAPI.
func NewResourcesAPI(c *client.Client) *ResourcesAPI {
	return &ResourcesAPI{client: c}
}

// K8sResource represents a K8s resource node from the graph API.
type K8sResource struct {
	UID       string `json:"id"`
	Name      string `json:"label"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
}

// graphNodesResponse mirrors the API wrapper for graph/nodes.
type graphNodesResponse struct {
	Nodes      []K8sResource `json:"nodes"`
	Edges      []TopoEdge    `json:"edges"`
	TotalCount int           `json:"totalCount"`
}

// ListPods retrieves Pod resources, optionally filtered by namespace.
func (a *ResourcesAPI) ListPods(namespace string) ([]K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=Pod"
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	return a.listResources(path)
}

// ListNodes retrieves Node resources.
func (a *ResourcesAPI) ListNodes() ([]K8sResource, error) {
	return a.listResources("/k8s/resources/graph/nodes?kind=Node")
}

// ListDeployments retrieves Deployment resources, optionally filtered by namespace.
func (a *ResourcesAPI) ListDeployments(namespace string) ([]K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=Deployment"
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	return a.listResources(path)
}

// ListStatefulSets retrieves StatefulSet resources, optionally filtered by namespace.
func (a *ResourcesAPI) ListStatefulSets(namespace string) ([]K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=StatefulSet"
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	return a.listResources(path)
}

// ListDaemonSets retrieves DaemonSet resources, optionally filtered by namespace.
func (a *ResourcesAPI) ListDaemonSets(namespace string) ([]K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=DaemonSet"
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	return a.listResources(path)
}

// ListServices retrieves Service resources, optionally filtered by namespace.
func (a *ResourcesAPI) ListServices(namespace string) ([]K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=Service"
	if namespace != "" {
		path += "&namespace=" + namespace
	}
	return a.listResources(path)
}

// GetPod retrieves a single Pod by name and namespace.
func (a *ResourcesAPI) GetPod(name, ns string) (*K8sResource, error) {
	path := fmt.Sprintf("/k8s/resources/graph/nodes?kind=Pod&namespace=%s", ns)
	resources, err := a.listResources(path)
	if err != nil {
		return nil, err
	}
	for i := range resources {
		if resources[i].Name == name {
			return &resources[i], nil
		}
	}
	return nil, fmt.Errorf("pod %s not found in namespace %s", name, ns)
}

// GetNode retrieves a single Node by name.
func (a *ResourcesAPI) GetNode(name string) (*K8sResource, error) {
	path := "/k8s/resources/graph/nodes?kind=Node"
	resources, err := a.listResources(path)
	if err != nil {
		return nil, err
	}
	for i := range resources {
		if resources[i].Name == name {
			return &resources[i], nil
		}
	}
	return nil, fmt.Errorf("node %s not found", name)
}

// TopoGraphNode represents a node in the topology graph.
type TopoGraphNode struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
}

// TopoEdge represents an edge in the topology graph.
type TopoEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label"`
}

// TopoGraph represents a topology graph response.
type TopoGraph struct {
	Nodes []TopoGraphNode `json:"nodes"`
	Edges []TopoEdge      `json:"edges"`
}

// topoGraphRaw mirrors the server response for graph/search and graph/nodes.
type topoGraphRaw struct {
	Nodes      []TopoGraphNode `json:"nodes"`
	Edges      []TopoEdge      `json:"edges"`
	TotalCount int             `json:"totalCount"`
}

// TopoGet retrieves topology around a resource by UID and depth.
func (a *ResourcesAPI) TopoGet(uid string, depth int) (*TopoGraph, error) {
	path := fmt.Sprintf("/k8s/resources/graph/search?resourceId=%s&depth=%d", uid, depth)
	return a.getTopoGraph(path)
}

// TopoSearch searches topology by kind and name.
func (a *ResourcesAPI) TopoSearch(kind, name string) (*TopoGraph, error) {
	path := fmt.Sprintf("/k8s/resources/graph/nodes?kind=%s", kind)
	graph, err := a.getTopoGraph(path)
	if err != nil {
		return nil, err
	}
	// Filter nodes by name client-side.
	var filtered []TopoGraphNode
	for _, n := range graph.Nodes {
		if n.Label == name {
			filtered = append(filtered, n)
		}
	}
	if name != "" {
		graph.Nodes = filtered
	}
	return graph, nil
}

// getTopoGraph performs an HTTP request and unmarshals a TopoGraph.
func (a *ResourcesAPI) getTopoGraph(path string) (*TopoGraph, error) {
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var raw topoGraphRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &TopoGraph{Nodes: raw.Nodes, Edges: raw.Edges}, nil
}

// listResources is a helper that performs the HTTP request and unmarshals the response.
func (a *ResourcesAPI) listResources(path string) ([]K8sResource, error) {
	data, err := a.client.Do(context.Background(), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp graphNodesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}
