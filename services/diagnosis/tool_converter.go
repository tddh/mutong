package diagnosis

import (
	"gitee.com/tddh/mutong/models/diagnosis"
)

func ConvertMCPToolsToOpenAI(tools []diagnosis.ToolDefinition) []map[string]interface{} {
	var result []map[string]interface{}
	for _, t := range tools {
		props := map[string]interface{}{}
		required := []string{}
		for _, p := range t.Parameters {
			props[p.Name] = map[string]interface{}{
				"type":        "string",
				"description": p.Description,
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": props,
					"required":   required,
				},
			},
		})
	}
	return result
}
