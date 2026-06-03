# Roadmap

## v0.1.0 — 初始公开发布（当前）

- [x] K8s 资源采集与 NebulaGraph 拓扑存储
- [x] 交互式拓扑可视化（G6 / force-graph / d3）
- [x] 告警收敛与混合 AI 诊断引擎（Eino ReAct Agent）
- [x] 声明式巡检系统（YAML 规则 + Cron 调度）
- [x] 自愈执行器（11 种操作类型，手动/自动双模式）
- [x] 复盘分析（事件时间线 + 因果链 DAG + 混合检索）
- [x] MCP 工具服务器（20 个内置工具）
- [x] 业务拓扑（Beyla eBPF + OTel Collector 自动发现）
- [x] Web 终端 + 日志查询 + 指标监控 + 链路追踪
- [x] CLI 工具 mutongctl（19 个命令模块）
- [x] RBAC 权限（admin / operator / viewer）

## v0.2.0 — 增强

- [ ] 用户/角色 CRUD（POST/PUT/DELETE）
- [ ] 前端测试覆盖
- [x] Docker 一键部署
- [x] CI 安全扫描（gosec + govulncheck）
- [ ] 诊断模块单元测试 Phase 2-4

## v0.3.0 — 扩展

- [x] 多集群 kubeconfig 声明式配置
- [ ] 告警 Webhook 签名验证
- [ ] API 文档全量 Swagger 注解
- [ ] 前端 ESLint + Prettier 集成

## 未来

- 多租户支持
- 自定义仪表盘
- 移动端适配
