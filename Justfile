# 牧童 (Mutong) 项目构建命令
# 使用 just 命令运行器

# 变量定义
output_dir := "artifacts"
output := "artifacts/mutong-linux-amd64"
source := "./cmd/main.go"

# 默认命令：显示帮助
default:
    @just --list

# ═══════════════════════════════════════════════════════════════
# 文档命令
# ═══════════════════════════════════════════════════════════════

# 生成 Swagger API 文档
swagger:
    swag init -g cmd/main.go -o docs --parseDependencyLevel 1 --parseInternal

# ═══════════════════════════════════════════════════════════════
# 构建命令
# ═══════════════════════════════════════════════════════════════

# 编译项目（生成 Linux amd64/arm64 和 macOS arm64 二进制文件）
build:
    mkdir -p {{output_dir}}
    go mod tidy
    gofmt -w .
    # Linux x86_64
    GOOS=linux GOARCH=amd64 go build -mod=mod -o {{output_dir}}/mutong-linux-amd64 {{source}}
    # Linux ARM64
    #GOOS=linux GOARCH=arm64 go build -mod=mod -o {{output_dir}}/mutong-linux-arm64 {{source}}
    # macOS ARM64 (Apple Silicon M1/M2/M3)
    GOOS=darwin GOARCH=arm64 go build -mod=mod -o {{output_dir}}/mutong-darwin-arm64 {{source}}
    # 同步配置文件
    cp -r configs {{output_dir}}/
    mkdir -p {{output_dir}}/configs/prompts && cp -r configs/prompts/* {{output_dir}}/configs/prompts/
    @echo "✅ 构建完成:"
    @ls -lh {{output_dir}}/mutong-*

# 仅构建 macOS 版本（本地开发调试）
build-mac:
    mkdir -p {{output_dir}}
    go mod tidy
    GOOS=darwin GOARCH=arm64 go build -mod=mod -o {{output_dir}}/mutong-darwin-arm64 {{source}}
    cp -r configs {{output_dir}}/
    mkdir -p {{output_dir}}/configs/prompts && cp -r configs/prompts/* {{output_dir}}/configs/prompts/
    @echo "✅ macOS 构建完成:"
    @ls -lh {{output_dir}}/mutong-darwin-arm64

# 编译项目（带SkyWalking代理）
build-skywalking:
    mkdir -p {{output_dir}}
    go mod tidy
    gofmt -w .
    GOOS=linux GOARCH=amd64 go build -toolexec="skywalking-go-agent-0.6.0 -config=agent.yaml" -a -o {{output_dir}}/mutong-linux-amd64-skywalking {{source}}
    GOOS=linux GOARCH=arm64 go build -toolexec="skywalking-go-agent-0.6.0 -config=agent.yaml" -a -o {{output_dir}}/mutong-linux-arm64-skywalking {{source}}
    cp -r configs {{output_dir}}/
    mkdir -p {{output_dir}}/configs/prompts && cp -r configs/prompts/* {{output_dir}}/configs/prompts/

# 清理构建产物
clean:
    rm -rf {{output_dir}}

# ═══════════════════════════════════════════════════════════════
# 测试命令
# ═══════════════════════════════════════════════════════════════

# 运行所有单元测试
test:
    go test -v -race ./...

# 运行测试并生成覆盖率报告
test-cover:
    go test -v -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "覆盖率报告已生成: coverage.html"

# 运行特定模块的测试
test-module module:
    go test -v -race ./{{module}}/...

# 运行基准测试
bench:
    go test -bench=. -benchmem ./...

# 运行告警系统API测试脚本
test-alert:
    ./test_alert.sh

# 运行动态冒烟测试（从 API 动态发现资源）
smoke-test:
    chmod +x scripts/smoke_test.sh
    ./scripts/smoke_test.sh

# ═══════════════════════════════════════════════════════════════
# 前端命令
# ═══════════════════════════════════════════════════════════════

# 前端开发模式（热重载）
dev-ui:
    cd view && bun run dev

# 前端构建
build-ui:
    mkdir -p {{output_dir}}/view
    cd view && OUT_DIR=../{{output_dir}}/view bun run build

# 前端预览
preview-ui:
    cd view && bun run preview

# 运行前端测试（需配置vitest后）
test-ui:
    cd view && bun run test

# ═══════════════════════════════════════════════════════════════
# 数据库命令
# ═══════════════════════════════════════════════════════════════

# 初始化 Nebula Graph 数据库
init-nebula:
    chmod +x scripts/update_nebula.sh
    cd scripts && ./update_nebula.sh

# 更新 Nebula Graph nGQL 语句
update-nebula:
    chmod +x scripts/update_nebula.sh
    cd scripts && ./update_nebula.sh

# 初始化 Elasticsearch 索引模板
init-es-template:
    chmod +x scripts/init_es_template.sh
    ./scripts/init_es_template.sh

# ═══════════════════════════════════════════════════════════════
# 运行命令
# ═══════════════════════════════════════════════════════════════

# 运行项目（使用 configs/ 目录模式）
run:
    ./{{output}} -c configs/

# 运行项目（指定配置文件）
run-config config:
    ./{{output}} -c {{config}}

# 运行项目（调试模式）
run-debug:
    ./{{output}} -c configs/ -log-level debug

# ═══════════════════════════════════════════════════════════════
# 开发工具
# ═══════════════════════════════════════════════════════════════

# 运行 JavaScript 测试文件
run-js:
    node view/test.js

# 格式化代码
fmt:
    gofmt -w .
    @echo "代码格式化完成"

# 检查代码静态分析
lint:
    go vet ./...

# ═══════════════════════════════════════════════════════════════
# 一键命令
# ═══════════════════════════════════════════════════════════════

# 完整构建流程：格式化 + 测试 + 构建
all: fmt test build
    @echo "✅ 构建完成: {{output}}"

# 完整开发流程：构建 + 前端构建
full-build: build build-ui
    @echo "✅ 后端和前端构建完成"

# ═══════════════════════════════════════════════════════════════
# mutongctl CLI 命令
# ═══════════════════════════════════════════════════════════════

cli-source := "./cmd/mutongctl"

cli-skill-src := ".opencode/skills/mutongctl/SKILL.md"
cli-skill-dst := "{{output_dir}}/skills/mutongctl"

# 构建 CLI（跨平台）
build-cli:
    mkdir -p {{output_dir}}
    GOOS=linux GOARCH=amd64 go build -mod=mod -o {{output_dir}}/mutongctl-linux-amd64 {{cli-source}}
    GOOS=darwin GOARCH=arm64 go build -mod=mod -o {{output_dir}}/mutongctl-darwin-arm64 {{cli-source}}
    mkdir -p {{cli-skill-dst}}
    cp {{cli-skill-src}} {{cli-skill-dst}}/SKILL.md
    @echo "✅ CLI 构建完成"

# 仅构建 macOS CLI（本地开发）
build-cli-mac:
    mkdir -p {{output_dir}}
    GOOS=darwin GOARCH=arm64 go build -mod=mod -o {{output_dir}}/mutongctl-darwin-arm64 {{cli-source}}
    mkdir -p {{cli-skill-dst}}
    cp {{cli-skill-src}} {{cli-skill-dst}}/SKILL.md
    @echo "✅ CLI macOS 构建完成"

# CLI 测试
test-cli:
    go test -v -race ./cmd/mutongctl/...

# CLI 一键：测试 + 构建
all-cli: test-cli build-cli
