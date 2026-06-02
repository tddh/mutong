# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o mutong ./cmd/main.go

# Stage 2: Runtime
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/mutong .
COPY --from=builder /app/configs ./configs

EXPOSE 8888

CMD ["./mutong", "-c", "configs/"]
