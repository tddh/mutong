package models

// BusinessApp 业务应用实体
// 对应 NebulaGraph 中的 BusinessApp 顶点
// 用于表示一个业务应用，包含应用名称、所属团队、业务单元、重要等级等属性
type BusinessApp struct {
	UID          string `json:"uid" nebula:"uid"`
	AppName      string `json:"app_name" nebula:"app_name"`
	Namespace    string `json:"namespace" nebula:"namespace"`
	Criticality  string `json:"criticality" nebula:"criticality"`
	Environment  string `json:"environment" nebula:"environment"`
	Team         string `json:"team" nebula:"team"`
	BusinessUnit string `json:"business_unit" nebula:"business_unit"`
}

// BelongsToAppEdge 资源属于业务应用的边
// 对应 NebulaGraph 中的 BelongsToApp 边 (K8sResource -> BusinessApp)
type BelongsToAppEdge struct {
	FromUID string `json:"from_uid"` // K8sResource UID
	ToUID   string `json:"to_uid"`   // BusinessApp UID
}

// CallsAppEdge 应用间调用关系边
// 对应 NebulaGraph 中的 CallsApp 边 (BusinessApp -> BusinessApp)
// 仅存关系骨架，调用指标由 Prometheus 承担
type CallsAppEdge struct {
	FromUID string `json:"from_uid"` // 调用方 BusinessApp UID
	ToUID   string `json:"to_uid"`   // 被调用方 BusinessApp UID
}
