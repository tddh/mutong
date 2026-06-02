# 重明 (Mutong) 待办事项

> 最后更新：2026-05-30

---

## 已完成（本轮）

| 工作项 |
|--------|
| **硬编码导出**：新增 ServerConfig，扩展 KafkaConfig/ExecutorConf，17 处硬编码替换为可配置项 |
| **执行器操作类型扩展**：5→11 种操作类型（新增 ConfigMap/Secret/ResourceLimits/Image/Annotations/Labels 操作） |
| **巡检规则页面配置**：GORM 模型 + RuleStore + DBBackedRule + CRUD API（5 个端点） |
| **ES 日志索引映射**：serviceToIndex 路由 + SearchLogsByBusiness + init_es_template 脚本 |
| 修复 config.infra.yaml deleteResource→delete_pod key 不匹配 bug |
| NavBar sticky 全局粘性定位 |
| MCP 工具调用全链路 Info 日志（InvokableRun + ExecuteTool） |
| Pod 终止态过滤（list_resources_from_cache + list_k8s_resources） |
| 三份文档全面审查修正（README / 架构 / 用户手册） |
| docs/ 目录清理归档（58文件 → 10保留） |
| configs/ 与 config 结构体完整对齐（18键零缺失） |
| 删除 RuleGo 遗留文件 + MySQL 脚本 |
| NavBar 补全 4 个缺失页面链接（监控/终端/追踪/集群） |
| api.js 补充 clusters 对象（修复集群管理页面报错） |
| 诊断 api.js 清理遗留端点 |
| config.inspection.yaml 字段修正（schedule→cronSpec） |
| 新建 config.retrospective.yaml |
| API 请求日志中间件（gin.Logger） |
| 健康检查增加 NebulaGraph 连通性（SHOW SPACES） |
| 拓扑页侧边栏资源类型筛选控件 |
| 配置启动校验（validate 必填字段） |
| 用户手册补充：部署组件 + Prompt 模板 |
| **CLI 文档全量补充**：README / 用户手册 / 架构文档均加入 mutongctl 信息，CLI_PLAN 更新实现状态 |
| **P2 LLM token 追踪**：TokenTrackingChatModel 包装器，记录每次 LLM 调用 token + 日志 + Prometheus |
| **P3 巡检历史前端**：新增 inspection-history 页面（列表/详情/对比/趋势） |
| **P3 指标覆盖率扩展**：StatefulSet/DaemonSet/Service 全链路指标支持 |
| **P3 优雅停机验证**：修复 6 个 goroutine 泄漏 + 重构 initializeGin() |
| **P3 API 文档生成**：引入 swaggo，Swagger UI 就绪 |
| **P3 诊断单元测试**：新建 test_helpers.go 共享 Mock 基础设施 |
| **P3 巡检持久化修复**：DB 接入/列名匹配/ID 防碰撞/GetLatestReport DB fallback |

---

## 待完成

### P2（中等优先级）

- [ ] **用户/角色 CRUD**：当前仅 GET /users 和 GET /roles，需补充 POST/PUT/DELETE + DB 表操作  
  *涉及文件：`controllers/user_controller.go`、`controllers/role_controller.go`、`models/`*

### P3（低优先级）

- [ ] **诊断模块单元测试 Phase 2-4**：engine.go / context_collector.go / eino_agent.go 补测  
  *涉及文件：`services/diagnosis/*_test.go`*

- [ ] **API 文档全量注解**：当前仅完成项目级配置和 Swagger UI，handler 注解待全面推进

---

## 开源准备

> 创建于 2026-05-30，共 10 项，合计估时约 9h。

### P0（阻塞开源）

- [x] **SECURITY.md 修复**：邮箱改为 mutong_cm@163.com + 移除公开的已知漏洞列表
- [x] **CODE_OF_CONDUCT.md**：Contributor Covenant 2.1 模板，联系人 mutong_cm@163.com
- [ ] ~~**模块路径迁移**~~（暂缓）：`gitee.com/tddh/mutong` → 目标平台路径  
  *涉及：`go.mod` 第 1 行 + 120+ 个 .go 文件的 import 语句 + README/CONTRIBUTING 中的 clone URL*
- [ ] **Dockerfile + docker-compose.yml**：一键部署体验  
  *涉及：新建 `Dockerfile`（多阶段构建）、`docker-compose.yml`（PG + Nebula + Kafka + Redis + Mutong）*  
  *估时：1h*

### P1（开源前完成）

- [x] **gosec + CI 安全扫描**：`.golangci.yml` 启用 gosec + CI 加 govulncheck + CodeQL  
  *涉及：`.golangci.yml`、`.github/workflows/ci.yml`*  
  *估时：1.5h*
  *结果：gosec 扫描零安全问题，.golangci.yml 已配置*
- [ ] **初始版本标签**：`git tag v0.1.0` + Release 说明  
  *估时：10min*
- [ ] **前端测试框架搭建**：vitest + @vue/test-utils + jsdom，为核心页面写冒烟测试  
  *涉及：`view/package.json`、新建 `vitest.config.ts`、`view/src/**/*.test.ts`*  
  *估时：3h*
- [x] **文档脱敏**：替换内部团队名称 → 通用示例名（已完成，11 文件 56 处）  
  *涉及：`docs/PROJECT_ARCHITECTURE.md`、`docs/alert-routing-usage-manual.md`、`configs/config.business.yaml.example`、`models/alert/alert_source_test.go`、`docs/superpowers/plans/*.md`*  
  *估时：1h*
- [ ] **替换废弃依赖 gogo/protobuf**：→ `google.golang.org/protobuf`  
  *涉及：`go.mod` + 依赖该库的源文件*  
  *估时：1h*

### P2（锦上添花）

- [ ] **SUPPORT.md + ROADMAP.md**：说明获取帮助的渠道 + 提炼 TODO 为面向用户的路线图  
  *估时：20min*
- [ ] **截图补充**：`docs/images/` 下 6 张占位图 → 真实截图（仪表盘/拓扑/诊断/告警/巡检/复盘）  
  *估时：20min*
- [ ] **Dependabot + pre-commit hooks**：Go + Actions 依赖自动更新 + gofmt/golangci-lint/prettier 提交前检查  
  *涉及：新建 `.github/dependabot.yml`、`.pre-commit-config.yaml`*  
  *估时：20min*
