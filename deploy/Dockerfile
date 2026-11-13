# ─── 阶段 1：Go 编译 ───
# 交叉编译：buildx 传入 TARGETOS/TARGETARCH，普通 docker build 时为空即本机平台
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS go-builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /mutong ./cmd/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /mutongctl ./cmd/mutongctl

# ─── 阶段 2：前端构建 ───
# 静态产物与 CPU 架构无关，固定在构建平台执行，避免 QEMU 模拟
FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS ui-builder

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

# 前端静态文件（二进制同级 view/dist，resolveViewDir 据此定位）
COPY --from=ui-builder /dist /app/view/dist

# 初始化脚本：首次启动时从 .example 复制配置
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && chown -R mutong:mutong /app

USER mutong
EXPOSE 8888

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8888/healthz || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["/app/mutong"]