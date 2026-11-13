# Stage 1: Build
# 交叉编译：buildx 传入 TARGETOS/TARGETARCH，普通 docker build 时为空即本机平台
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o mutong ./cmd/main.go

# Stage 2: 前端构建（静态产物与架构无关，固定构建平台执行）
FROM --platform=$BUILDPLATFORM oven/bun:1-alpine AS ui-builder

WORKDIR /ui
COPY view/package.json view/bun.lock ./
RUN bun install --frozen-lockfile

COPY view/ ./
RUN OUT_DIR=/dist bun run build

# Stage 3: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    adduser -D -h /app mutong

WORKDIR /app

COPY --from=builder /app/mutong .
COPY --from=builder /app/configs ./configs
COPY --from=ui-builder /dist ./view/dist
COPY deploy/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh && chown -R mutong:mutong /app

USER mutong
EXPOSE 8888

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8888/healthz || exit 1

ENTRYPOINT ["./entrypoint.sh"]
CMD ["./mutong"]