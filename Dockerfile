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

# Stage 2: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    adduser -D -h /app mutong

WORKDIR /app

COPY --from=builder /app/mutong .
COPY --from=builder /app/configs ./configs
COPY deploy/entrypoint.sh ./entrypoint.sh
RUN chmod +x ./entrypoint.sh && chown -R mutong:mutong /app

USER mutong
EXPOSE 8888

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8888/healthz || exit 1

ENTRYPOINT ["./entrypoint.sh"]
CMD ["./mutong"]