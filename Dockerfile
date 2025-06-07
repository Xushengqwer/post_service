# Dockerfile

# ---- 第一阶段：构建阶段 ----
# 使用官方 Go 镜像作为构建环境，选择与您开发环境匹配的版本
FROM golang:1.23.7-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum 文件，并下载依赖项
# 这样做可以利用 Docker 的层缓存，只有在依赖变化时才重新下载
COPY go.mod go.sum ./
RUN go mod download

# 复制所有源代码到工作目录
# 注意：如果您有 .dockerignore 文件来排除不需要的文件（如 .git, docker-data），会更好
COPY . .

# 构建 Go 应用程序
# - CGO_ENABLED=0 禁用 CGO，使得构建静态链接的可执行文件更容易
# - ldflags "-s -w" 移除调试信息，减小最终镜像大小
# -o /app/post_service 指定输出的可执行文件名
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/post_service ./main.go

# ---- 第二阶段：运行阶段 ----
# 使用一个非常小的基础镜像来运行编译好的程序
FROM alpine:latest

# 设置工作目录
WORKDIR /app

# 从构建阶段 (builder) 复制编译好的可执行文件
COPY --from=builder /app/post_service /app/post_service

# 复制配置文件目录 (假设应用会从 /app/config 读取)
# 注意：配置文件通常在运行时通过 docker-compose 的 volumes 挂载，
# 但如果需要默认配置或模板，可以在这里复制。
# COPY config /app/config # 根据需要取消注释或修改

# 暴露应用程序监听的端口 (与 docker-compose 中的 ports 对应)
EXPOSE 8082

# 定义容器启动时执行的命令
# 运行我们编译好的 Go 程序
# 您可能需要传递命令行参数，例如配置文件的路径
CMD ["/app/post_service", "-config=/app/config/config.development.yaml"]
# 或者，如果您的配置是通过环境变量读取的，这里可能只需要 CMD ["/app/post_service"]