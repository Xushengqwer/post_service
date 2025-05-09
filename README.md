# Post Service (帖子服务)

## 描述

本项目是一个使用 Go 语言构建的后端服务，专注于处理与帖子相关的功能，例如发布、查询、管理和展示帖子。它被设计为一个微服务，可以独立运行或集成到更大的系统中。

## 主要功能

* **帖子管理:** 支持帖子的创建、读取和软删除操作。
* **管理员功能:**
    * 按多种条件分页查询帖子列表。
    * 审核帖子（通过/拒绝）。
    * 更新帖子的官方标签。
    * 管理员执行帖子软删除。
* **热门帖子:**
    * 通过游标分页加载热门帖子列表。
    * 获取热门帖子的详细信息.
* **浏览量统计:**
    * 使用 Redis ZSet 维护帖子排名。
    * 使用 Redis String 计数器和 Bloom Filter 实现防刷的浏览量增加.
    * 定时任务将 Redis 中的浏览量**高效、并发地**同步回 **MySQL 数据库 (支持读写分离)**。
* **性能优化:**
    * **MySQL 读写分离:** 提高数据库的并发处理能力和响应速度。
    * **并发执行 MySQL 批量写:** 显著提升 Redis 到 MySQL 数据同步的性能。
* **异步处理:** 使用 Kafka 处理帖子创建后的审核请求和帖子删除事件。
* **API 文档:** 通过 Swagger 提供 API 文档。

## 技术栈

* **语言:** Go
* **Web 框架:** Gin
* **ORM:** GORM
* **数据库:** MySQL (**支持读写分离**)
* **缓存/数据结构:** Redis (包括 Redis Stack 功能如 Bloom Filter)
* **消息队列:** Kafka
* **日志:** Zap
* **配置:** YAML, Viper
* **追踪:** OpenTelemetry
* **API 文档:** Swaggo
* **容器化:** Docker, Docker Compose

## 环境要求

* Go (请查看 `go.mod` 文件以了解推荐版本)
* Docker
* Docker Compose

## 安装与运行

1.  **克隆仓库:**

    ```bash
    git clone <your-repository-url>
    cd post_service
    ```
2.  **配置:**
    * 检查并根据需要修改 `config/config.development.yaml` 文件，特别是数据库（**包括读写分离配置**）、Redis 和 Kafka 的连接信息（如果您的 Docker 环境不同）。确保配置与 `docker-compose.yaml` 文件中的服务设置匹配。
3.  **启动依赖服务:**
    * 使用 Docker Compose 启动 MySQL, Redis 和 Kafka。

    ```bash
    docker-compose up -d
    ```

    * 初次启动 Kafka 可能需要一些时间来完成初始化和领导者选举。
4.  **运行 Go 应用:**

    ```bash
    go run main.go -config=config/config.development.yaml
    ```

    * 服务将在配置文件中指定的端口（默认为 8080）上启动。

## API 文档

服务启动后，可以通过浏览器访问 `/swagger/index.html` 来查看和交互式测试 API。

## 项目结构 (概览)

```
post_service/
├── cmd/            # 插入假数据测试的程序入口 (main.go, seeder)
├── config/         # 配置文件 (YAML) 和 Go 配置结构体
├── constant/       # 项目常量 (Redis Keys, Kafka Topics, Cron Specs 等)
├── controller/     # HTTP 请求处理器 (Gin handlers)
├── dependencies/   # 依赖初始化 (数据库, Redis 连接等)
├── docs/           # Swagger 文档文件 (自动生成)
├── docker-compose.yaml # Docker Compose 配置文件
├── docker-data/    # Docker 持久化数据 (建议加入 .gitignore)
├── go.mod          # Go 模块文件
├── go.sum          # Go 模块校验和
├── main.go         # 应用主入口
├── middleware/     # Gin 中间件
├── models/         # 数据模型 (DTOs, Entities, VO, Enums)
├── mq/             # 消息队列相关 (生产者, 消费者)
├── repo/           # 数据仓库层 (数据库和缓存操作)
├── service/        # 业务逻辑层
├── tasks/          # 后台定时任务 (**包括高效的 Redis 到 MySQL 数据同步**)
└── README.md       # (您正在阅读的文件)

```

## 贡献

欢迎提交 Pull Request 或 Issue 来改进项目。

## 许可证

本项目采用 Apache 2.0 许可证。