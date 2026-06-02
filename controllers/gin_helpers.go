package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// QueryIntDefault 获取查询参数整数值，带默认值
func QueryIntDefault(c *gin.Context, key string, def int) int {
	if v := c.Query(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// QueryInt64Default 获取查询参数 int64 值，带默认值
func QueryInt64Default(c *gin.Context, key string, def int64) int64 {
	if v := c.Query(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return def
}

// extractQueryParams 从 gin.Context 提取所有查询参数，用于构建 RequestContext
func extractQueryParams(c *gin.Context) map[string]string {
	params := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}

// ErrorJSON 统一返回 JSON 错误响应
func ErrorJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
