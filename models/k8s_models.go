package models

import (
	"k8s.io/apimachinery/pkg/types"
)

/*
各类型资源做为tag来进行映射？
目前边的属性值作为标签，边作为关系，边的属性作为属性
*/
type Relationship struct {
	From types.UID         `json:"from"`
	To   types.UID         `json:"to"`
	Attr map[string]string `json:"attr"`
}

// 映射到nebula graph中的tag
type K8sResource struct {
	Uid            string `json:"uid" nebula:"uid"`
	Cluster        string `json:"cluster" nebula:"cluster"`
	Name           string `json:"name" nebula:"name"`
	Group          string `json:"api_group" nebula:"api_group"`
	Labels         string `json:"labels" nebula:"labels"`
	NameSpace      string `json:"name_space" nebula:"name_space"`
	ResourceDefine string `json:"resource_define" nebula:"resource_define"`
	Kind           string `json:"kind" nebula:"kind"`
	APIVersion     string `json:"api_version,omitempty" nebula:"api_version"`
	IsDeleted      bool   `json:"is_deleted" nebula:"is_deleted"`
	DeletedAt      int64  `json:"deleted_at" nebula:"deleted_at"`
}

/*
	type Dependency struct {
		ResourceType   string      `json:"resource_type"`
		ResourceName   string      `json:"resource_name"`
		ResourceDefine interface{} `json:"resource_define"`
	}

	type K8sResourceGraph struct {
		Nodes []*K8sResource `json:"nodes"`
		Edges []*Edge        `json:"edges"`
	}

	type Edge struct {
		Source string   `json:"source"`
		Target string   `json:"target"`
		Tag    []string `json:"tag"`
	}
*/
type KafkaResourceMessage struct {
	EventType string `json:"event_type"`
	Group     string `json:"group"`
	Cluster   string `json:"cluster"`
	Object    []byte `json:"object"`
}
