# Changelog

## [Unreleased]

### Added
- 多集群 kubeconfig 声明式配置
- CI/CD 自动化（GitHub Actions，含 lint/test/build）
- golangci-lint 静态分析配置
- 英文 README
- 自愈执行器操作类型扩展：5→11 种（新增 updateConfigMap / updateSecret / updateResourceLimits / updateDeploymentImage / updateAnnotations / updateLabels）
- 巡检规则 CRUD API（GORM 模型 + RuleStore + DBBackedRule + 5 个端点）
- ES 日志索引映射：serviceToIndex 路由 + SearchLogsByBusiness + init_es_template 脚本
- 巡检历史前端页面（列表/详情/对比/趋势分析）
- 指标覆盖率扩展：StatefulSet/DaemonSet/Service 全链路指标支持
- Swagger API 文档生成（swaggo 集成，`just swagger`）
- LLM Token 追踪：TokenTrackingChatModel 包装器，记录每次 LLM 调用 token + 日志 + Prometheus
- 配置启动校验（必填字段 validate）
- 拓扑页侧边栏资源类型筛选控件
- 健康检查增加 NebulaGraph 连通性（SHOW SPACES）
- API 请求日志中间件（gin.Logger）
- config.retrospective.yaml 复盘自动触发配置
- CLI mutongctl 工具（18 个命令模块，4 种输出格式）
- NavBar 补全 4 个缺失页面链接（监控/终端/追踪/集群）
- 诊断模块单元测试（Phase 1，test_helpers.go 共享 Mock 基础设施）
- 冒烟测试脚本（动态 API 发现资源）

### Changed
- 配置统一为 `configs/` 目录模式，配置文件提供 `.example` 模板
- LICENSE 确定为 MIT
- 硬编码导出：新增 ServerConfig，扩展 KafkaConfig/ExecutorConf，17 处硬编码替换为可配置项
- 三份文档全面审查修正（README / 架构 / 用户手册）
- docs/ 目录清理归档（58 文件 → 10 保留）
- configs/ 与 config 结构体完整对齐（18 键零缺失）
- MCP 工具调用全链路 Info 日志（InvokableRun + ExecuteTool）

### Fixed
- 无 kubeconfig 时优雅降级不再 crash
- kubeconfig 敏感信息脱敏
- config.infra.yaml deleteResource→delete_pod key 不匹配 bug
- NavBar sticky 全局粘性定位
- Pod 终止态过滤（list_resources_from_cache + list_k8s_resources）
- api.js 补充 clusters 对象（修复集群管理页面报错）
- 诊断 api.js 清理遗留端点
- config.inspection.yaml 字段修正（schedule→cronSpec）
- 优雅停机验证：修复 6 个 goroutine 泄漏 + 重构 initializeGin()
- 巡检持久化修复：DB 接入/列名匹配/ID 防碰撞/GetLatestReport DB fallback

### Removed
- `config.yaml` 单体配置文件
- RuleGo 遗留文件 + MySQL 脚本
