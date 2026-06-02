# Contributing to Mutong

感谢你对重明 (Mutong) 的关注！

## 开发环境

```bash
git clone https://gitee.com/tddh/mutong.git
cd mutong

# 初始化配置
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
cp agent.yaml.example agent.yaml

# 安装依赖
go mod download
cd view && bun install && cd ..

# 运行
just build-mac && just run
```

## 代码规范

- `just fmt` — 格式化（gofmt）
- `just lint` — 静态检查（go vet）
- `just test` — 运行测试（-race）
- 所有导出函数、类型、常量需添加注释

## 提交流程

1. Fork 仓库
2. 创建特性分支 (`git checkout -b feat/xxx`)
3. 提交变更 (`git commit -m 'feat: xxx'`)
4. 推送到分支 (`git push origin feat/xxx`)
5. 提交 Pull Request

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
