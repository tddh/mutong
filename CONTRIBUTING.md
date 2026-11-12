# Contributing to Mutong

> 名字出处：「离，丽也。日月丽乎天，百谷草木丽乎土，重明以丽乎正，乃化成天下。」——《易经·离卦·彖传》

感谢你对重明 (Mutong) 的关注！

## 开发环境

前置条件：**Go 1.27**（`go.mod` 声明 `go 1.27.0`）、**Bun 1.x+**、**Just**、Node 24（前端测试，见 `.nvmrc`）。NebulaGraph 3.8+ 与 Kafka（或 Redpanda）是运行期硬依赖，本地可用 `docker compose up -d` 起依赖。

```bash
git clone https://gitee.com/tddh/mutong.git
cd mutong

# 初始化配置
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
cp agent.yaml.example agent.yaml

# 安装依赖
go mod download
cd view && bun install && cd ..

# 本地开发密钥（不要提交到 Git；生产部署见 README「安全配置」）
export MUTONG_SESSION_SECRET="$(openssl rand -hex 32)"
export MUTONG_INIT_ADMIN_PASSWORD='<强密码>'

# 初始化 NebulaGraph Schema
just init-nebula

# 运行
just build-mac && just run
```

`just build-mac` 产出 darwin arm64 二进制；`just run` 按宿主机平台自动选择 `artifacts/mutong-<os>-<arch>` 并以 `-c configs/` 目录模式启动。Linux x86_64 用 `just build`，其他平台用 `just build-host`。

> `just build` 只做编译与配置同步，**不再内嵌 `go mod tidy` / `gofmt`**——构建不应改写 `go.mod` / `go.sum`。需要时显式执行 `just tidy` 与 `just fmt`。

只想起依赖、用 IDE 跑后端时，先用 `docker compose up -d` 起 PostgreSQL / NebulaGraph（metad + storaged + graphd）/ Redpanda / Redis，再本地运行：

```bash
export POSTGRES_PASSWORD='<数据库密码>'
export NEBULA_PASSWORD='<Nebula 密码>'
export MUTONG_REDIS_PASSWORD='<Redis 密码>'
docker compose up -d
go run ./cmd/main.go -c configs/
```

## 代码规范

- `just fmt` — 格式化（gofmt）
- `just tidy` — 整理依赖（go mod tidy）
- `just lint` — 静态检查（go vet）；CI 另跑 golangci-lint（含 gosec）
- `just test` — 运行测试（-race）
- `cd view && bun run lint && bun run test` — 前端 ESLint + vitest
- 所有导出函数、类型、常量需添加注释
- nGQL 字符串字面量必须走 `pkg/ngql` sanitizer，不要手工拼接或 `strconv.Quote`
- 新增路由默认受鉴权保护；确需公开时在 `controllers/auth_middleware.go` 的 `isPublicPath` 显式登记并说明理由

## 提交流程

1. Fork 仓库
2. 创建特性分支 (`git checkout -b feat/xxx`)
3. 提交变更 (`git commit -m 'feat: xxx'`)
4. 推送到分支 (`git push origin feat/xxx`)
5. 提交 Pull Request（目标分支 `develop`）

提交前请自检：这次改动是否改变了使用预期？若是，同步更新文档——架构/路由变更更新 `docs/PROJECT_ARCHITECTURE.md`，功能与配置变更更新 `README.md` + `README.en.md`，完成项更新 `ROADMAP.md` / `docs/TODO.md`。详见 `.agents/AGENTS.md`。

## Commit 规范

使用 [Conventional Commits](https://www.conventionalcommits.org/)：

- `feat:` 新功能
- `fix:` 修复
- `docs:` 文档
- `refactor:` 重构
- `chore:` 构建/工具
- `test:` 测试

## 问题反馈

通过 [Gitee Issues](https://gitee.com/tddh/mutong/issues) 提交 Bug 报告或功能建议。

**安全漏洞不要走公开 Issue**，请按 [SECURITY.md](SECURITY.md) 的流程私下报告。