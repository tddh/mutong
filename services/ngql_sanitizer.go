package services

import (
	"strings"
	"unicode"
)

// NGQLSanitizer 提供 nGQL 安全转义功能
// 防止 nGQL 注入攻击
type NGQLSanitizer struct{}

// NewNGQLSanitizer 创建转义器实例
func NewNGQLSanitizer() *NGQLSanitizer {
	return &NGQLSanitizer{}
}

// EscapeString 安全转义字符串用于 nGQL 查询
// nGQL 字符串使用单引号，需要转义单引号和反斜杠
func (s *NGQLSanitizer) EscapeString(str string) string {
	if str == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(str) + 10)

	for _, r := range str {
		switch r {
		case '\'':
			// 单引号转义为 \\'
			builder.WriteString("\\'")
		case '\\':
			// 反斜杠转义为 \\\\
			builder.WriteString("\\\\")
		case '\n':
			builder.WriteString("\\n")
		case '\r':
			builder.WriteString("\\r")
		case '\t':
			builder.WriteString("\\t")
		default:
			// 过滤控制字符
			if unicode.IsPrint(r) {
				builder.WriteRune(r)
			}
		}
	}

	return builder.String()
}

// QuoteString 安全地将字符串包装为 nGQL 字符串字面量
// 返回带单引号的字符串，如 'escaped_string'
func (s *NGQLSanitizer) QuoteString(str string) string {
	return "'" + s.EscapeString(str) + "'"
}

// ValidateIdentifier 验证标识符是否安全
// 用于验证 Tag 名称、Edge 名称、属性名称等
func (s *NGQLSanitizer) ValidateIdentifier(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}

	// 只允许字母、数字、下划线
	for i, r := range id {
		if i == 0 {
			// 首字符必须是字母或下划线
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}

	// 检查是否为 nGQL 保留字
	reserved := map[string]bool{
		"GO": true, "FROM": true, "TO": true, "WHERE": true,
		"MATCH": true, "RETURN": true, "WITH": true, "UNWIND": true,
		"INSERT": true, "UPDATE": true, "DELETE": true, "DROP": true,
		"CREATE": true, "ALTER": true, "SHOW": true, "DESC": true,
		"AND": true, "OR": true, "NOT": true, "IN": true,
		"AS": true, "ORDER": true, "BY": true, "LIMIT": true,
		"SKIP": true, "GROUP": true, "YIELD": true, "FETCH": true,
		"LOOKUP": true, "FIND": true, "IF": true, "ELSE": true,
	}

	return !reserved[strings.ToUpper(id)]
}

// BuildSafeQuery 构建安全的 nGQL 查询
// 参数: template - 查询模板，使用 $1, $2 等占位符
// 参数: args - 要替换的参数列表
func (s *NGQLSanitizer) BuildSafeQuery(template string, args ...string) string {
	result := template
	for i, arg := range args {
		placeholder := "$" + string(rune('1'+i))
		result = strings.ReplaceAll(result, placeholder, s.QuoteString(arg))
	}
	return result
}

// WhitelistKind 验证 K8s 资源类型是否在白名单中
// 用于防止 kind 参数注入
func WhitelistKind(kind string) (string, bool) {
	allowedKinds := map[string]string{
		"Pod":         "Pod",
		"Node":        "Node",
		"Service":     "Service",
		"Deployment":  "Deployment",
		"ConfigMap":   "ConfigMap",
		"Secret":      "Secret",
		"Ingress":     "Ingress",
		"PVC":         "PersistentVolumeClaim",
		"PV":          "PersistentVolume",
		"Namespace":   "Namespace",
		"StatefulSet": "StatefulSet",
		"DaemonSet":   "DaemonSet",
		"Job":         "Job",
		"CronJob":     "CronJob",
	}

	safe, ok := allowedKinds[kind]
	return safe, ok
}
