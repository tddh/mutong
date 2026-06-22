# Changelog

## [Unreleased]

### Added
- AI 诊断聊天会话持久化：`chat_sessions` 表新增 `user_id`/`title` 字段，`chat/ask` 自动创建/续接会话
- 会话历史 API：`GET /chat/sessions`（当前用户列表）、`GET /chat/sessions/:id`（详情）、`DELETE /chat/sessions/:id`（软删除）
- 侧栏会话列表（前端）：新建对话 / 切换会话 / 删除，刷新页面不丢失
- LLM 智能标题摘要（Eino chatModel，15 字内中文，异步非阻塞生成）
- PG 会话定时清理（每小时，720h 保留期），Redis 密码认证支持
- Server `sseWriteTimeoutSec` 配置（SSE 流式写超时，默认 300s）
- 外部知识库搜索（Tavily + GitHub Issues）：MCP 工具 search_knowledge_base / search_github_issues，支持 AI 诊断中 LLM 自动调用及 mutongctl 手动触发
- mutongctl search 命令（github / tavily 子命令）
- HTTP 连接池统一工厂 `services/httpclient`（共享 Transport，MaxIdleConns: 100）
- WebSocket 终端保活机制（Ping/Pong + ReadDeadline + WriteDeadline + 会话过期清理）
- 前端 ESLint + Prettier 代码规范配置
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
- 资源 Profiles 拆分为 configs/profiles/*.yml 独立文件（pod/node/deployment/service/statefulset/daemonset/pvc/ingress），按资源类型独立配置 PromQL 指标
- 巡检规则 YAML 文件加载：configs/rules/inspection/*.yml，5 类内置规则独立文件，加载优先级 YAML > Go 内置 > DB

### Changed
- `chat/ask` 从无状态改为自动创建/续接用户会话（登录后首次消息即创建，侧栏同步显示）
- 移除前端快捷诊断按钮和引导面板，改为会话侧栏（新建/切换/删除）
- `chat_sessions` 表新增 `user_id:uint`（联合索引 `idx_user_status`）+ `title:varchar(255)` 字段
- 会话 PG 保留期从 30 分钟改为 30 天
- cmdb_data_silo 巡检规则排除 CRD/FlowSchema/PriorityLevelConfiguration/Lease 资源类型
- 巡检报告前端改为表格展示（级别/规则/资源/建议 4 列）

### Fixed
- 修复 ClusterRoleBinding→ClusterRole 关联查询 bug（obj.Name → obj.RoleRef.Name），大量 ClusterRole 不再误报为孤岛
- 巡检报告 YAML 规则 Resource 字段补充 namespace 信息
- 外部搜索配置：`enabled` 改为 `true`，YAML key 统一为 camelCase，支持 MUTONG_TAVILY_KEY / MUTONG_GITHUB_TOKEN 环境变量覆盖
- 诊断 System Prompt 增加外部搜索工具引导
- 配置统一为 `configs/` 目录模式，配置文件提供 `.example` 模板
- LICENSE 确定为 MIT
- 硬编码导出：新增 ServerConfig，扩展 KafkaConfig/ExecutorConf，17 处硬编码替换为可配置项
- 三份文档全面审查修正（README / 架构 / 用户手册）
- docs/ 目录清理归档（58 文件 → 10 保留）
- configs/ 与 config 结构体完整对齐（18 键零缺失）
- MCP 工具调用全链路 Info 日志（InvokableRun + ExecuteTool）

### Fixed
- GitHub Issues 搜索返回 422：Fine-grained PAT 缺少 `is:issue` 限定符，已自动追加
- GitHub 搜索客户端：添加 User-Agent + 错误响应 body 读取
- 搜索客户端增加结构化日志
- Alert 聚合器竞态条件：done channel 锁外读 → 局部变量 + Stop() 加 sync.Once
- 测试 mock 并发 write（mockNotifier / mockStorage 加 sync.Mutex）
- saveOAuthToken Token 刷新不持久化（预存 bug：map 副本未写回）
- OAuth Token 刷新 HTTP 请求无超时（http.PostForm → 显式构造 + 30s 超时）
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
