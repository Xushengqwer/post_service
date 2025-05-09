package main

import (
	_ "github.com/Xushengqwer/post_service/docs" // 确保导入了 docs 包

	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// 导入项目包
	appConfig "github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/controller"
	"github.com/Xushengqwer/post_service/dependencies"
	// "post_service/middleware" // middleware 在 router 中使用
	// "post_service/models/entities"
	"github.com/Xushengqwer/post_service/mq/consumer"
	"github.com/Xushengqwer/post_service/mq/producer"
	// "post_service/myErrors"
	"github.com/Xushengqwer/post_service/repo/mysql"
	redisrepo "github.com/Xushengqwer/post_service/repo/redis"
	"github.com/Xushengqwer/post_service/router"
	"github.com/Xushengqwer/post_service/service"
	"github.com/Xushengqwer/post_service/tasks"

	// 导入公共模块
	sharedCore "github.com/Xushengqwer/go-common/core"
	sharedTracing "github.com/Xushengqwer/go-common/core/tracing"

	// 导入 OTel HTTP Client Instrumentation
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	// 导入 Zap
	"go.uber.org/zap"
)

// @title           Post Service API
// @version         1.0
// @description     帖子服务，提供帖子发布、查询、管理等功能。
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support Team
// @contact.url    https://github.com/Xushengqwer/go-backend-learning
// @contact.email  your_support_email@example.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// API 的主机和端口 (根据开发环境配置)  <--- 将注释移到下一行

// @BasePath  /api/v1
// API 的基础路径                   <--- 将注释移到下一行

func main() {
	// --- 配置和基础设置 ---
	var configFile string
	flag.StringVar(&configFile, "config", "config/config.development.yaml", "Path to configuration file")
	flag.Parse()

	// 1. 加载配置
	var cfg appConfig.PostConfig
	if err := sharedCore.LoadConfig(configFile, &cfg); err != nil {
		log.Fatalf("FATAL: 加载配置失败 (%s): %v", configFile, err)
	}
	// log.Printf("DEBUG: Loaded config: %+v\n", cfg) // 避免打印敏感信息

	// 2. 初始化 Logger
	logger, loggerErr := sharedCore.NewZapLogger(cfg.ZapConfig)
	if loggerErr != nil {
		log.Fatalf("FATAL: 初始化 ZapLogger 失败: %v", loggerErr)
	}
	defer func() {
		logger.Info("正在同步日志...")
		if err := logger.Logger().Sync(); err != nil {
			log.Printf("WARN: ZapLogger Sync 失败: %v\n", err)
		}
	}()
	logger.Info("Logger 初始化成功")

	// 3. 初始化 TracerProvider (启用)
	// TODO: otelTransport 用于需要追踪的 HTTP Client (例如服务间出站调用)，该服务目前暂时没有出站的请求
	// var otelTransport http.RoundTripper = http.DefaultTransport // <--- 暂时不再需要声明这个变量
	var tracerShutdown func(context.Context) error // 用于优雅关停
	if cfg.TracerConfig.Enabled {                  // 使用配置中的 TracerConfig
		var err error
		tracerShutdown, err = sharedTracing.InitTracerProvider(
			constant.ServiceName,
			constant.ServiceVersion,
			cfg.TracerConfig, // 传递配置
		)
		if err != nil {
			logger.Fatal("初始化 TracerProvider 失败", zap.Error(err))
		}
		// 使用 defer 确保追踪系统在程序退出时关闭
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			logger.Info("正在关闭 TracerProvider...")
			if err := tracerShutdown(ctx); err != nil {
				logger.Error("关闭 TracerProvider 失败", zap.Error(err))
			} else {
				logger.Info("TracerProvider 已成功关闭")
			}
		}()
		logger.Info("分布式追踪已初始化")
		// 修正：由于当前服务不主动发起 HTTP 调用，暂时不创建 instrumentedHttpClient
		// 仅初始化 Transport 并赋值给 _ 以满足 OTel 可能的内部需求或避免编译错误
		_ = otelhttp.NewTransport(http.DefaultTransport)
		logger.Debug("OTel HTTP Transport 初始化完成 (暂未使用)")

	} else {
		logger.Info("分布式追踪已禁用")
		tracerShutdown = func(ctx context.Context) error { return nil } // 提供一个空操作关闭函数

	}

	// --- 4. 初始化核心依赖 ---
	// 4.1 数据库 (MySQL)
	db, dbErr := dependencies.InitMySQL(&cfg, logger)
	if dbErr != nil {
		logger.Fatal("初始化 MySQL 数据库失败", zap.Error(dbErr))
	}
	logger.Info("MySQL 数据库连接成功")

	// 4.2 Redis
	rdb, redisErr := dependencies.InitRedis(&cfg.RedisConfig, logger)
	if redisErr != nil {
		logger.Fatal("初始化 Redis 失败", zap.Error(redisErr))
	}
	logger.Info("Redis 连接成功")

	// 4.3 Kafka 生产者
	var kafkaProducer *producer.KafkaProducer
	if len(cfg.KafkaConfig.Brokers) > 0 {
		kafkaProducer = producer.NewKafkaProducer(cfg.KafkaConfig, logger)
		logger.Info("Kafka 生产者已初始化")
	} else {
		logger.Warn("未配置 Kafka brokers，Kafka 生产者将为 nil")
	}

	// --- 5. 初始化数据仓库层 (Repositories) ---
	postRepo := mysql.NewPostRepository(db, logger)
	postDetailRepo := mysql.NewPostDetailRepository(db)
	postAdminRepo := mysql.NewPostAdminRepository(db, logger)
	logger.Debug("MySQL Repositories 初始化完成")

	postViewRepo := redisrepo.NewPostViewRepository(
		rdb, logger,
		constant.BloomFilterDefaultSize, // 使用常量
		constant.BloomFilterDefaultHashes,
		constant.BloomFilterDefaultErrorRate,
	)
	cacheRepo := redisrepo.NewCache(postViewRepo, postRepo, postDetailRepo, rdb, logger)
	logger.Debug("Redis Repositories 初始化完成")

	// --- 6. 初始化服务层 (Services) ---
	postService := service.NewPostService(db, postRepo, postDetailRepo, postViewRepo, kafkaProducer, logger)
	hotPostService := service.NewPostQueryService(cacheRepo, postViewRepo, logger)
	postAdminService := service.NewPostAdminService(postAdminRepo, postRepo, postDetailRepo, logger, db, kafkaProducer)
	logger.Debug("Services 初始化完成")

	// --- 7. 初始化控制器层 (Controllers) ---
	postController := controller.NewPostController(postService)
	hotPostController := controller.NewHotPostController(hotPostService)
	postAdminController := controller.NewPostAdminController(postAdminService)
	logger.Debug("Controllers 初始化完成")

	// --- 8. 初始化 Kafka 消费者 ---
	var kafkaConsumer *consumer.Consumer
	var consumerWg sync.WaitGroup
	var consumerCancel context.CancelFunc // 用于通知消费者 goroutine 停止

	if len(cfg.KafkaConfig.Brokers) > 0 && cfg.KafkaConfig.Topics.PostAuditResult != "" {
		auditResultHandler := consumer.NewAuditResultHandler(logger, postAdminService)
		var consumerErr error
		// 使用配置中的 ConsumerGroupID
		groupID := cfg.KafkaConfig.ConsumerGroupID
		if groupID == "" {
			logger.Warn("Kafka ConsumerGroupID 未在配置中设置，将使用默认或空值")
			// 可以设置一个默认值或返回错误
			// groupID = "post_service_default_group"
		}
		kafkaConsumer, consumerErr = consumer.NewConsumer(
			&cfg.KafkaConfig,
			groupID,                               // 从配置读取
			constant.KafkaTopicKeyPostAuditResult, // 使用常量作为 Key
			auditResultHandler,
			logger,
		)
		if consumerErr != nil {
			logger.Fatal("初始化 Kafka 消费者失败", zap.Error(consumerErr))
		}

		// 启动消费者 goroutine
		consumerWg.Add(1)
		go func() {
			defer consumerWg.Done()
			// 创建一个可取消的 context 传递给 Start
			var consumerCtx context.Context
			consumerCtx, consumerCancel = context.WithCancel(context.Background()) // 将 cancel 赋值给外部变量
			logger.Info("Kafka 消费者 goroutine 正在启动...")
			kafkaConsumer.Start(consumerCtx)
			logger.Info("Kafka 消费者 goroutine 已退出")
		}()
		logger.Info("Kafka 消费者已初始化并启动 goroutine")
	} else {
		logger.Warn("未配置 Kafka 或 PostAuditResult 主题，不启动消费者")
	}

	// --- 9. 初始化定时任务 ---
	syncTask := tasks.NewSyncService(postViewRepo, postRepo, logger)
	cacheTask := tasks.NewHotPostsCacheTask(cacheRepo, logger)
	logger.Info("后台定时任务已初始化并启动")

	// --- 10. 设置 Gin 路由器 ---
	// 将初始化好的控制器传递给 SetupRouter
	ginRouter := router.SetupRouter(logger, &cfg, postController, hotPostController, postAdminController)
	logger.Info("Gin 路由器已设置")

	// --- 11. 启动 HTTP 服务器 ---
	serverAddr := fmt.Sprintf(":%s", cfg.ServerConfig.Port)
	httpServer := &http.Server{
		Addr:    serverAddr,
		Handler: ginRouter,
	}

	// 启动 HTTP 服务器 goroutine
	go func() {
		logger.Info("HTTP 服务器开始监听", zap.String("address", serverAddr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP 服务器启动失败", zap.Error(err))
		}
		logger.Info("HTTP 服务器已停止监听")
	}()

	// --- 12. 实现优雅关停 ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	receivedSignal := <-quit
	logger.Info("收到关停信号，开始优雅退出...", zap.String("signal", receivedSignal.String()))

	// 创建关停超时 context
	shutdownCtx, shutdownCancelFunc := context.WithTimeout(context.Background(), 30*time.Second) // 30 秒关停超时
	defer shutdownCancelFunc()

	// a. 停止 HTTP 服务器 (允许处理完当前请求)
	logger.Info("正在关闭 HTTP 服务器...")
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("关闭 HTTP 服务器失败", zap.Error(err))
	} else {
		logger.Info("HTTP 服务器已成功关闭")
	}

	// b. 停止 Kafka 消费者
	if kafkaConsumer != nil {
		logger.Info("正在关闭 Kafka 消费者...")
		if consumerCancel != nil { // 调用之前保存的 cancel 函数来停止 Start 循环
			consumerCancel()
		}
		// 可以选择性地调用 Close (取决于 Start 的实现是否需要 Close 来释放资源)
		if err := kafkaConsumer.Close(); err != nil {
			logger.Error("关闭 Kafka 消费者 Client 失败", zap.Error(err))
		}
		// 等待消费者 goroutine 真正结束
		logger.Info("等待 Kafka 消费者 goroutine 退出...")
		consumerWg.Wait() // 等待 Add(1) 对应的 Done()
		logger.Info("Kafka 消费者已停止")
	}

	// c. 停止定时任务调度器 (等待任务结束)
	logger.Info("正在停止定时任务...")
	syncStopCtx := syncTask.Stop()
	cacheStopCtx := cacheTask.Stop()

	// 使用 select 和 定时器来等待任务结束，避免无限阻塞
	tasksStopped := 0
	for tasksStopped < 2 { // 等待两个任务结束
		select {
		case <-syncStopCtx.Done():
			logger.Info("浏览量同步任务已停止")
			syncStopCtx = nil // 防止重复 select 到
			tasksStopped++
		case <-cacheStopCtx.Done():
			logger.Info("热帖缓存任务已停止")
			cacheStopCtx = nil // 防止重复 select 到
			tasksStopped++
		case <-shutdownCtx.Done(): // 检查总的关停超时
			logger.Error("等待定时任务停止超时", zap.Error(shutdownCtx.Err()))
			tasksStopped = 2 // 超时则强制退出等待
		}
		// 如果一个 context 已经是 nil，则短暂 sleep 避免空转 CPU
		if syncStopCtx == nil && cacheStopCtx == nil {
			break // 都完成了
		} else if (syncStopCtx == nil && cacheStopCtx != nil) || (syncStopCtx != nil && cacheStopCtx == nil) {
			// 如果一个完成一个没完成，短暂 sleep 等待另一个或超时
			time.Sleep(100 * time.Millisecond)
		}
	}
	logger.Info("所有定时任务已停止")

	// d. (其他清理，例如关闭 TracerProvider - 已通过 defer 处理)

	logger.Info("服务已成功关闭")
}
