# 项目指令 / Project Rules

## 🇨🇳 中文回复规则 / Chinese Response Rule（最高优先级 / Highest Priority）

**所有对话输出必须使用中文（简体中文） / All conversation output MUST be in Chinese (Simplified)**：

- 思考过程与推理 / Thinking and reasoning
- 解释与讨论 / Explanations and discussions
- 代码注释（除非团队规范要求英文）/ Code comments (unless team convention requires English)
- Git 提交信息 / Git commit messages
- 错误说明 / Error descriptions

例外 / Exceptions：代码本身（变量名、函数名）遵循项目规范 / Code itself (variable names, function names) follows project conventions；URL/技术标识符保持原样 / URLs and technical identifiers remain as-is。

---

## 文档维护规则 / Documentation Maintenance Rules

**核心原则 / Core Principle**：每次代码改动如果改变了用户或未来开发者的使用预期，必须同步更新文档。
Every code change that alters user or future developer expectations MUST synchronously update documentation.

### ✅ 必须更新文档 / Must Update Docs

- **架构调整**（新增/删除服务、数据流变更）/ **Architecture changes** (new/removed services, data flow changes)：更新 `docs/PROJECT_ARCHITECTURE.md` + 相关设计文档 / Update `docs/PROJECT_ARCHITECTURE.md` + related design docs
- **新增配置项** / **New config items**：更新 `docs/` 相关 spec + 示例配置 / Update relevant specs + example config
- **API 变更**（新增/修改路由）/ **API changes** (new/modified routes)：更新 `docs/PROJECT_ARCHITECTURE.md` API 表 / Update API table in `docs/PROJECT_ARCHITECTURE.md`
- **新增子模块** / **New sub-modules**：创建新文档 + 更新索引 / Create new doc + update index

### ❌ 不需要更新文档 / No Need to Update Docs

- **修复 Bug** / **Bug fixes**：行为修正 / Behavior correction
- **重构** / **Refactoring**：不改行为的内部调整 / Internal changes without behavior impact
- **样式调整** / **Style changes**：纯 UI 或日志格式 / Pure UI or log formatting

### ✍️ 提交前自检 / Pre-Commit Self-Check

每次 `git commit` 前，必须自检 / Before every `git commit`, self-check：

> "这次改动是否改变了使用预期？" / "Does this change alter usage expectations?"
> * 是 / Yes → 同步更新文档 / Synchronize docs
> * 否 / No → 不需要 / Not needed

---

## 方案确认流程 / Proposal Confirmation Process（强制执行 / Mandatory）

**核心原则 / Core Principle**：任何涉及架构调整、新功能实现、性能优化的方案，必须经过用户明确确认后才能实施。
Any proposal involving architecture changes, new features, or performance optimization MUST receive explicit user confirmation before implementation.

### 🚫 禁止行为 / Forbidden Behavior

- 未经用户确认直接开始实现代码 / Starting code implementation without user confirmation
- 假设用户意图并自行选择方案 / Assuming user intent and selecting a solution unilaterally
- 在用户未明确回复"确认"、"执行"、"开始"等指令前动手 / Acting before the user explicitly replies with "确认/confirm", "执行/execute", "开始/start"

### ✅ 必须执行的流程 / Required Process

1. **提出问题** / **Identify the problem**：发现需要解决的架构/功能问题 / Identify an architecture/feature problem
2. **分析方案** / **Analyze solutions**：提供 2-3 种可行方案，包含优缺点对比 / Provide 2-3 feasible solutions with pros/cons comparison
3. **等待确认** / **Wait for confirmation**：明确询问用户选择哪个方案 / Explicitly ask which solution the user prefers
4. **收到确认** / **Receive confirmation**：用户明确回复方案选择（如"方案 A"、"用方案 B"、"确认执行"）/ User explicitly replies with choice (e.g., "方案 A", "用方案 B", "确认执行")
5. **开始实施** / **Begin implementation**：仅在收到确认后开始编码 / Only start coding after confirmation

### 📋 确认示例 / Confirmation Examples

**正确流程 / Correct Process**：
```
AI: 我发现了问题 X，有 3 种解决方案... 你选择哪个？
    I found problem X. There are 3 solutions... Which one do you choose?
用户/User: 方案 A / Solution A
AI: 好的，开始实施方案 A... / OK, implementing Solution A...
```

**错误流程 / Incorrect Process**：
```
AI: 我发现了问题 X，有 3 种解决方案... 我先实施方案 A
    I found problem X. There are 3 solutions... I'll implement Solution A first
用户/User: （未确认 / No confirmation）
AI: （直接开始写代码 / Starts coding immediately）← 禁止 / FORBIDDEN
```

### ⚠️ 适用范围 / Scope

以下场景必须走确认流程 / The following scenarios MUST go through confirmation：
- 新增功能/模块 / New features/modules
- 架构调整（数据流变更、服务依赖变更）/ Architecture changes (data flow, service dependency)
- 性能优化方案选择 / Performance optimization approach selection
- 数据库 Schema 变更 / Database schema changes
- 第三方依赖引入 / Third-party dependency introduction
- 配置结构变更 / Configuration structure changes

以下场景可直接实施 / The following scenarios can proceed directly：
- 修复明确的 Bug（用户已描述问题现象）/ Fixing clear bugs (user has described the issue)
- 代码重构（不改变行为）/ Code refactoring (no behavior change)
- 文档更新 / Documentation updates
- 测试用例补充 / Test case additions
