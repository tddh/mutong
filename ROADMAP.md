# Roadmap

> 版本状态以 `CHANGELOG.md` 与代码为准。当前处于 **v0.1.0 发布准备期**：`[Unreleased]` 中的安全加固与发布工程已落地，即将打 `v0.1.0` tag 并通过 `.github/workflows/release.yml` 发布到 `ghcr.io`。

## v0.1.0 — 初始公开发布（即将 tag）

- [x] K8s 资源采集与 NebulaGraph 拓扑存储
- [x] 交互式拓扑可视化（G6 / force-graph / d3）
- [x] 告警收敛与混合 AI 诊断引擎（Eino ReAct Agent）
- [x] 声明式巡检系统（YAML 规则 + Cron 调度，3 类检查类型 / 6 类内置规则）
- [x] 自愈执行器（13 种操作类型，手动/自动双模式 + 执行后验证与自动回滚）
- [x] 复盘分析（事件时间线 + 因果链 DAG + 混合检索）
- [x] MCP 工具服务器（29 个工具：22 诊断 + 7 执行类提议）
- [x] 业务拓扑（Beyla eBPF + OTel Collector 自动发现，含 L4 flow 拓扑）
- [x] Web 终端 + 日志查询 + 指标监控 + 链路追踪
- [x] CLI 工具 mutongctl（20 个命令模块）
- [x] RBAC 权限（admin / operator / viewer，Casbin enforce/shadow/off）
- [x] 外部知识检索（Tavily + GitHub Issues，含 PII 脱敏与审计）
- [x] 鉴权默认拒绝 + 会话 HMAC 验签 + 执行器 admin 门禁
- [x] 告警 Webhook HMAC-SHA256 签名（时间戳参与签名）+ 重放拒绝 + 1MB body 上限
- [x] `GET /healthz` 无鉴权存活探针
- [x] Docker 一键部署（NebulaGraph metad/storaged/graphd 齐全，口令必填）
- [x] CI（lint / test / build / frontend / docker）+ govulncheck 漏洞扫描 job
- [x] 发布流水线 `release.yml`（tag `v*` 触发，镜像推送 ghcr.io）

## v0.2.0 — 增强

- [ ] 用户/角色 CRUD（POST/PUT/DELETE）——当前 `controllers/user_controller.go`、`role_controller.go` 仍只注册 GET
- [x] 前端测试覆盖（vitest，14 个测试文件，CI frontend job 跑 lint + test + build）
- [x] Docker 一键部署
- [x] CI 安全扫描（golangci-lint 启用 gosec + 独立 govulncheck job）
- [ ] 诊断模块单元测试 Phase 2-4——`engine_test.go` 等 8 个测试文件已就位，`context_collector.go` / `eino_agent.go` 仍缺专测
- [ ] ServiceAccount 管理 API（模型与 AutoMigrate 已有，缺 `/api/auth/service-accounts` CRUD 端点）
- [ ] 巡检规则管理 UI（后端 CRUD + toggle 已完整，前端只读展示结果）
- [ ] 个人 Token 管理 UI（`/api/auth/tokens` 已支持列出/创建/撤销，无前端入口）

## v0.3.0 — 扩展

- [x] 多集群 kubeconfig 声明式配置
- [x] 告警 Webhook 签名验证（已随 v0.1.0 安全加固提前落地）
- [ ] API 文档全量 Swagger 注解——`just swagger` 与 UI 开关已就绪，handler 级 `@Summary` 注解尚未铺开
- [x] 前端 ESLint + Prettier 集成（`view/eslint.config.js`、`.prettierrc`，`bun run lint` / `format`）
- [ ] 多集群按 cluster 参数过滤（资源 API / RequestContext / 前端选择器）
- [ ] 监控页重建为 Grafana 内嵌（调研结论见 `docs/TODO.md`）

## 未来

- 多租户支持
- 自定义仪表盘
- 移动端适配
- 成本管理（OpenCost）、安全合规扫描（Kubescape）、SLO/SLI 管理——详见 `docs/TODO.md` 竞品差距章节