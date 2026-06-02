package interfaces

// RequestContext 封装 HTTP 请求上下文，解耦 Web 框架
type RequestContext struct {
	TraceID     string
	QueryParams map[string]string
}

// Query 获取查询参数值
func (rc RequestContext) Query(key string) string {
	return rc.QueryParams[key]
}

// QueryDefault 获取查询参数值，不存在时返回默认值
func (rc RequestContext) QueryDefault(key, def string) string {
	if v, ok := rc.QueryParams[key]; ok {
		return v
	}
	return def
}
