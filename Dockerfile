# post_service/Dockerfile (修正版)

# ---- 构建阶段 ----
FROM golang:1.23.7-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/post_service ./main.go

# ---- 运行阶段 ----
FROM alpine:latest

RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app

COPY --from=builder /app/post_service .
# [核心修正] 创建 config 目录，并只复制生产配置文件
RUN mkdir -p /app/config
COPY ./config/config.production.yaml /app/config/config.production.yaml

EXPOSE 8082
USER appuser

# [核心修正] 默认启动命令指向生产配置文件
CMD ["/app/post_service", "-config", "/app/config/config.production.yaml"]