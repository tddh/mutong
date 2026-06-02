package executor

import "time"

// ActionType defines the kind of remediation action
type ActionType string

const (
	// ActionRestartPod restarts a Pod
	ActionRestartPod ActionType = "restart_pod"
	// ActionScaleDeployment scales a Deployment
	ActionScaleDeployment ActionType = "scale_deployment"
	// ActionDeletePod deletes a Pod
	ActionDeletePod ActionType = "delete_pod"
	// ActionCreateHPA creates a HorizontalPodAutoscaler for a Deployment
	ActionCreateHPA ActionType = "create_hpa"
	// ActionUpdateHPA updates an existing HorizontalPodAutoscaler
	ActionUpdateHPA ActionType = "update_hpa"
	// ActionUpdateConfigMap updates a ConfigMap's data
	ActionUpdateConfigMap ActionType = "update_configmap"
	// ActionUpdateSecret updates a Secret's stringData
	ActionUpdateSecret ActionType = "update_secret"
	// ActionUpdateResourceLimits adjusts container CPU/Memory resource limits
	ActionUpdateResourceLimits ActionType = "update_resource_limits"
	// ActionUpdateDeploymentImage changes a Deployment's container image
	ActionUpdateDeploymentImage ActionType = "update_deployment_image"
	// ActionUpdateAnnotations patches resource annotations
	ActionUpdateAnnotations ActionType = "update_annotations"
	// ActionUpdateLabels patches resource labels
	ActionUpdateLabels ActionType = "update_labels"
)

// RiskLevel defines execution risk
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// ExecutionPlan describes a planned remediation action
type ExecutionPlan struct {
	ID            string            `json:"id"`
	Action        ActionType        `json:"action"`
	Target        string            `json:"target"` // e.g., namespace/name or resource identifier
	Namespace     string            `json:"namespace"`
	ResourceName  string            `json:"resourceName"`
	Reason        string            `json:"reason"` // AI diagnosis conclusion
	Confidence    float64           `json:"confidence"`
	Risk          RiskLevel         `json:"risk"`
	ApprovedBy    string            `json:"approvedBy"` // empty if auto, username if approved
	Replicas      int32             `json:"replicas"`   // desired number of replicas for deployments
	MinReplicas   int32             `json:"minReplicas"`
	MaxReplicas   int32             `json:"maxReplicas"`
	TargetCPU     int32             `json:"targetCpu"`
	TargetMemory  int32             `json:"targetMemory,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	ConfigData    map[string]string `json:"configData,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Image         string            `json:"image,omitempty"`
	ContainerName string            `json:"containerName,omitempty"`
}

// ExecutionResult records the outcome of an execution
type ExecutionResult struct {
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	AuditID   string    `json:"auditId"`
	Timestamp time.Time `json:"timestamp"`
}

// AuditLog stores execution history
type AuditLog struct {
	ID           string          `json:"id"`
	Plan         ExecutionPlan   `json:"plan"`
	Result       ExecutionResult `json:"result"`
	AutoExecuted bool            `json:"autoExecuted"`
	ApprovedBy   string          `json:"approvedBy,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
}
