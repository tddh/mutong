# ─── 阶段 1：Go 编译 ───
FROM golang:1.25-alpine AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /mutong ./cmd/main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /mutongctl ./cmd/mutongctl

# ─── 阶段 2：前端构建 ───
FROM oven/bun:1-alpine AS ui-builder

WORKDIR /ui
COPY view/package.json view/bun.lock ./
RUN bun install --frozen-lockfile

COPY view/ ./
RUN OUT_DIR=/dist bun run build

# ─── 阶段 3：运行镜像 ───
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    adduser -D -h /app mutong

WORKDIR /app

# 二进制
COPY --from=go-builder /mutong /app/mutong
COPY --from=go-builder /mutongctl /app/mutongctl

# 配置文件模板
COPY configs/ /app/configs/

# 前端静态文件（挂载到 /view 路径）
COPY --from=ui-builder /dist /app/view

# 初始化脚本：首次启动时从 .example 复制配置
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

USER mutong
EXPOSE 8888

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/mutong", "-c", "configs/"]
