package services

import (
	"fmt"
	"strings"
	"sync"
)

// QueryBuilder 提供 nGQL 查询构建的统一接口
// 统一处理字符串转义、引号风格、参数化查询
type QueryBuilder struct {
	sanitizer *NGQLSanitizer
}

var (
	queryBuilderInstance *QueryBuilder
	queryBuilderOnce     sync.Once
)

// GetQueryBuilder 获取全局 QueryBuilder 实例（单例）
func GetQueryBuilder() *QueryBuilder {
	queryBuilderOnce.Do(func() {
		queryBuilderInstance = &QueryBuilder{
			sanitizer: NewNGQLSanitizer(),
		}
	})
	return queryBuilderInstance
}

// Quote 包装字符串为 nGQL 字符串字面量
// 统一使用单引号，符合 nGQL 惯例
// 用法：Quote("my-pod") => 'my-pod'
func (b *QueryBuilder) Quote(s string) string {
	return b.sanitizer.QuoteString(s)
}

// QuoteVID 包装 VID（Vertex ID）
// VID 在 NebulaGraph 中可以是字符串或数字
// 对于字符串 VID，使用双引号包裹
func (b *QueryBuilder) QuoteVID(vid string) string {
	// NebulaGraph VID 使用双引号
	return `"` + b.sanitizer.EscapeString(vid) + `"`
}

// SafeMatch 构建 MATCH 查询
// 用法：SafeMatch("K8sResource", map[string]interface{}{"kind": "Pod", "name": "my-pod"})
func (b *QueryBuilder) SafeMatch(tag string, filters map[string]interface{}, returnFields []string) string {
	var builder strings.Builder
	builder.WriteString("MATCH (v:")
	builder.WriteString(tag)
	builder.WriteString("{")

	i := 0
	for key, value := range filters {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(key)
		builder.WriteString(":")
		builder.WriteString(b.formatValue(value))
		i++
	}

	builder.WriteString("}) RETURN ")

	for j, field := range returnFields {
		if j > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("v.")
		builder.WriteString(tag)
		builder.WriteString(".")
		builder.WriteString(field)
		builder.WriteString(" as ")
		builder.WriteString(field)
	}

	return builder.String()
}

// SafeInsertEdge 构建 INSERT EDGE 语句
// 用法：SafeInsertEdge("OwnedBy", "src-uid", "dst-uid", nil)
func (b *QueryBuilder) SafeInsertEdge(edgeType string, srcVID, dstVID string, props map[string]interface{}) string {
	var builder strings.Builder
	builder.WriteString("INSERT EDGE ")
	builder.WriteString(edgeType)

	if len(props) > 0 {
		builder.WriteString("(")
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		for i, k := range keys {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(k)
		}
		builder.WriteString(") VALUES ")
		builder.WriteString(b.QuoteVID(srcVID))
		builder.WriteString(" -> ")
		builder.WriteString(b.QuoteVID(dstVID))
		builder.WriteString(":(")
		for i, k := range keys {
			if i > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(b.formatValue(props[k]))
		}
		builder.WriteString(")")
	} else {
		builder.WriteString("() VALUES ")
		builder.WriteString(b.QuoteVID(srcVID))
		builder.WriteString(" -> ")
		builder.WriteString(b.QuoteVID(dstVID))
		builder.WriteString(":()")
	}

	return builder.String()
}

// SafeInsertVertex 构建 INSERT VERTEX 语句
func (b *QueryBuilder) SafeInsertVertex(tag string, vid string, props map[string]interface{}) string {
	var builder strings.Builder
	builder.WriteString("INSERT VERTEX ")
	builder.WriteString(tag)
	builder.WriteString("(")

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	for i, k := range keys {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(k)
	}

	builder.WriteString(") VALUES ")
	builder.WriteString(b.QuoteVID(vid))
	builder.WriteString(":(")

	for i, k := range keys {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(b.formatValue(props[k]))
	}
	builder.WriteString(")")

	return builder.String()
}

// SafeUpdateVertex 构建 UPDATE VERTEX 语句
func (b *QueryBuilder) SafeUpdateVertex(tag string, vid string, sets map[string]interface{}) string {
	var builder strings.Builder
	builder.WriteString("UPDATE VERTEX ON ")
	builder.WriteString(tag)
	builder.WriteString(" ")
	builder.WriteString(b.QuoteVID(vid))
	builder.WriteString(" SET ")

	i := 0
	for key, value := range sets {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(b.formatValue(value))
		i++
	}

	return builder.String()
}

// SafeLookup 构建 LOOKUP 查询
func (b *QueryBuilder) SafeLookup(tagOrEdge string, where map[string]interface{}, yieldFields []string) string {
	var builder strings.Builder
	builder.WriteString("LOOKUP ON ")
	builder.WriteString(tagOrEdge)
	builder.WriteString(" WHERE ")

	i := 0
	for key, value := range where {
		if i > 0 {
			builder.WriteString(" AND ")
		}
		builder.WriteString(tagOrEdge)
		builder.WriteString(".")
		builder.WriteString(key)
		builder.WriteString(" == ")
		builder.WriteString(b.formatValue(value))
		i++
	}

	builder.WriteString(" YIELD ")
	for j, field := range yieldFields {
		if j > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(tagOrEdge)
		builder.WriteString(".")
		builder.WriteString(field)
	}

	return builder.String()
}

// formatValue 格式化值为 nGQL 字面量
func (b *QueryBuilder) formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return b.Quote(v)
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%f", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return b.Quote(fmt.Sprintf("%v", v))
	}
}

// Escape 转义字符串（不添加引号）
func (b *QueryBuilder) Escape(s string) string {
	return b.sanitizer.EscapeString(s)
}

// ValidateIdentifier 验证标识符是否安全
func (b *QueryBuilder) ValidateIdentifier(id string) bool {
	return b.sanitizer.ValidateIdentifier(id)
}
