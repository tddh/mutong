package interfaces

import (
	"context"
)

type K8sResourceInterface interface {
	Get(RequestContext) (interface{}, error)
	Collect(string)
	CollectWithContext(context.Context, string)
	Relationship(string)
	GetAllResources(RequestContext) (interface{}, error)
	GetAllRelationships(RequestContext) (interface{}, error)
	GetKindsAndNamespaces(RequestContext) (interface{}, error)
	SearchResourceRelationship(RequestContext) (interface{}, error)
	GetResourceDefine(RequestContext) (interface{}, error)
	SuggestResources(RequestContext) (interface{}, error)
	ConsumeKafkaMessages()
	ConsumeKafkaMessagesWithContext(context.Context)
	Stop()
}
