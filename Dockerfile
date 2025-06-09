# post_service/Dockerfile (修正版)

# ---- 第一阶段：构建阶段 (Builder Stage) ----
# 使用官方 Go alpine 镜像作为构建环境
FROM golang:1.23.7-alpine AS builder

# 设置工作目录
WORKDIR /app

# 先复制 go.mod 和 go.sum 文件并下载依赖，以利用 Docker 的层缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制所有源代码到工作目录
COPY . .

# 构建 Go 应用程序，并移除调试信息以减小体积
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/post_service ./main.go

# ---- 第二阶段：运行阶段 (Runtime Stage) ----
# 使用一个非常小的 alpine 基础镜像
FROM alpine:latest

# [新增] 创建非 root 用户和组，增强安全性
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# 设置工作目录
WORKDIR /app

# [修正] 从构建阶段 (builder) 复制编译好的可执行文件
COPY --from=builder /app/post_service /app/post_service

# [核心修正] 创建 config 目录，并从构建阶段复制整个 config 目录
RUN mkdir -p /app/config
COPY --from=builder /app/config /app/config

# 暴露应用程序监听的端口
EXPOSE 8082

# [新增] 切换到非 root 用户运行
USER appuser

# [修正] 使用 exec 格式的 CMD，并指向正确的配置文件路径
CMD ["/app/post_service", "-config", "/app/config/config.development.yaml"]