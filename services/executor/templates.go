package executor

import (
	"encoding/json"
	"os"
	"sync"

	exModel "gitee.com/tddh/mutong/models/executor"
)

type HPATemplate struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	MinReplicas  int32  `json:"minReplicas"`
	MaxReplicas  int32  `json:"maxReplicas"`
	TargetCPU    int32  `json:"targetCpu"`
	TargetMemory int32  `json:"targetMemory,omitempty"`
}

type HPATemplates struct {
	Templates []HPATemplate `json:"templates"`
}

var (
	templates     *HPATemplates
	templatesOnce sync.Once
	templatesErr  error
)

func LoadHPATemplates(path string) (*HPATemplates, error) {
	templatesOnce.Do(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			templates = &HPATemplates{}
			templatesErr = err
			return
		}
		var t HPATemplates
		if err := json.Unmarshal(data, &t); err != nil {
			templates = &HPATemplates{}
			templatesErr = err
			return
		}
		templates = &t
		templatesErr = nil
	})
	return templates, templatesErr
}

func GetTemplate(name string) *HPATemplate {
	if templates == nil {
		return nil
	}
	for i := range templates.Templates {
		if templates.Templates[i].Name == name {
			return &templates.Templates[i]
		}
	}
	return nil
}

func GetDefaultTemplate() *HPATemplate {
	if templates == nil || len(templates.Templates) == 0 {
		return &HPATemplate{
			Name:        "default",
			Description: "默认模板",
			MinReplicas: 1,
			MaxReplicas: 5,
			TargetCPU:   50,
		}
	}
	return &templates.Templates[0]
}

func ApplyTemplate(template *HPATemplate, deploymentName, namespace string) *exModel.ExecutionPlan {
	return &exModel.ExecutionPlan{
		Action:       exModel.ActionCreateHPA,
		Namespace:    namespace,
		ResourceName: deploymentName,
		MinReplicas:  template.MinReplicas,
		MaxReplicas:  template.MaxReplicas,
		TargetCPU:    template.TargetCPU,
		TargetMemory: template.TargetMemory,
	}
}

func ListTemplates() []HPATemplate {
	if templates == nil {
		return nil
	}
	return templates.Templates
}
