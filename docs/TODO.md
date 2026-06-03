# 重明 (Mutong) 待办事项

> 最后更新：2026-06-03

---

## 待完成

### P1（高优先级）

- [ ] **初始版本标签**：`git tag v0.1.0` + Release 说明
  *估时：10min*

### P2（中等优先级）

- [ ] **前端页面交互优化**：15 个页面均为基础功能实现，缺乏交互体验优化。包括：加载状态/骨架屏、空状态占位、错误边界处理、表格分页/排序/筛选增强、表单验证反馈、动画过渡效果、响应式布局适配（平板/手机）、深色模式支持、键盘快捷键、无障碍访问（alt/aria 标签）
  *涉及：`view/src/` 下全部页面及共享组件*
  *估时：8h*

- [ ] **用户/角色 CRUD**：当前仅 GET /users 和 GET /roles，需补充 POST/PUT/DELETE + DB 表操作
  *涉及文件：`controllers/user_controller.go`、`controllers/role_controller.go`、`services/user_service.go`、`services/role_service.go`、`models/`*

- [ ] **ServiceAccount 管理 API**：当前 `models/service_account.go` 数据模型已定义且通过 GORM AutoMigrate，但缺少 CRUD 端点（`/api/auth/service-accounts`）。OAuth2 认证（fosite Provider、Zitadel OIDC、PAT/SAT Token）已完整实现
  *涉及文件：`controllers/auth_controller.go`（新增端点）、`services/auth/token.go`（已有 SAT 创建能力，需对接 API）*

- [ ] ~~**模块路径迁移**~~（暂缓）：`gitee.com/tddh/mutong` → 目标平台路径
  *涉及：`go.mod` 第 1 行 + 120+ 个 .go 文件的 import 语句 + README/CONTRIBUTING 中的 clone URL*

### P3（低优先级）

- [ ] **诊断模块单元测试 Phase 2-4**：engine.go / context_collector.go / eino_agent.go 补测
  *涉及文件：`services/diagnosis/*_test.go`*

- [ ] **API 文档全量注解**：当前仅完成项目级配置和 Swagger UI，handler 注解待全面推进

- [ ] **前端测试框架搭建**：vitest + @vue/test-utils + jsdom，为核心页面写冒烟测试
  *涉及：`view/package.json`、新建 `vitest.config.ts`、`view/src/**/*.test.ts`*
  *估时：3h*

- [ ] **替换废弃依赖 gogo/protobuf**：→ `google.golang.org/protobuf`
  *涉及：`go.mod` + 依赖该库的源文件*
  *估时：1h*
