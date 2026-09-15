package services

import (
	"gitee.com/tddh/mutong/pkg/ngql"
)

// NGQLSanitizer 提供 nGQL 安全转义功能
// 这是一个兼容性别名，实际实现已移动到 pkg/ngql 包
type NGQLSanitizer = ngql.Sanitizer

// NewNGQLSanitizer 创建转义器实例
// 这是一个兼容性别名，推荐直接使用 ngql.NewSanitizer()
func NewNGQLSanitizer() *NGQLSanitizer {
	return ngql.NewSanitizer()
}

// WhitelistKind 验证 K8s 资源类型是否在白名单中
// 这是一个兼容性别名，推荐直接使用 ngql.WhitelistKind()
func WhitelistKind(kind string) (string, bool) {
	return ngql.WhitelistKind(kind)
}
