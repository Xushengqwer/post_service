package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Xushengqwer/post_service/mq/producer"
	"os"
	"path/filepath"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"go.uber.org/zap"

	appConfig "github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/dependencies"
	"github.com/Xushengqwer/post_service/repo/mysql"
	redisRepo "github.com/Xushengqwer/post_service/repo/redis"
	postServicePkg "github.com/Xushengqwer/post_service/service"
)

func main() {
	// --- 0. 解析命令行参数 ---
	var numPosts int
	var configFile string
	flag.StringVar(&configFile, "config", "config/config.development.yaml", "配置文件路径")
	flag.IntVar(&numPosts, "n", 50, "要生成的帖子数量 (默认: 50)")
	var waitSeconds int
	flag.IntVar(&waitSeconds, "wait", 5, "数据填充后等待的秒数 (确保异步任务完成, 默认: 5秒)") // 新增等待时间参数
	flag.Parse()

	absConfigFile, err := filepath.Abs(configFile)
	if err != nil {
		fmt.Printf("无法获取配置文件的绝对路径 '%s': %v\n", configFile, err)
		absConfigFile = configFile
	}
	fmt.Printf("准备使用配置文件 '%s' (尝试绝对路径: '%s') 生成 %d 条测试帖子...\n", configFile, absConfigFile, numPosts)

	if numPosts <= 0 {
		fmt.Println("错误: 生成的帖子数量必须大于 0")
		os.Exit(1)
	}
	if waitSeconds < 0 {
		fmt.Println("错误: 等待秒数不能为负")
		os.Exit(1)
	}

	// --- 1. 加载配置 ---
	var cfg appConfig.PostConfig
	if err := core.LoadConfig(absConfigFile, &cfg); err != nil {
		fmt.Printf("加载配置失败 (%s): %v\n", absConfigFile, err)
		os.Exit(1)
	}
	fmt.Println("配置加载成功。")
	fmt.Printf("DEBUG: 从配置加载的 MySQL Write DSN: '%s'\n", cfg.MySQLConfig.Write.DSN)
	if cfg.MySQLConfig.Write.DSN == "" {
		fmt.Println("警告: MySQL Write DSN 为空。请检查：")
		fmt.Println("1. 配置文件路径是否正确 (当前尝试路径: ", absConfigFile, ")。")
		fmt.Println("2. 配置文件内容中 `mysql.write.dsn` 是否存在且有值。")
		fmt.Println("3. 是否有环境变量覆盖了此配置项为空字符串。")
	}

	// --- 2. 初始化日志记录器 ---
	logger, loggerErr := core.NewZapLogger(cfg.ZapConfig)
	if loggerErr != nil {
		fmt.Printf("初始化 ZapLogger 失败: %v\n", loggerErr)
		os.Exit(1)
	}
	defer func() {
		logger.Info("Seeder: 正在刷新日志...") // 增加退出前的日志刷新提示
		_ = logger.Logger().Sync()
		logger.Info("Seeder: 日志已刷新。")
	}()
	logger.Info("Logger 初始化成功 (Seeder)")

	// --- 3. 初始化 MySQL 数据库连接 ---
	db, dbErr := dependencies.InitMySQL(&cfg, logger)
	if dbErr != nil {
		logger.Fatal("初始化 MySQL 失败 (Seeder)", zap.Error(dbErr))
	}
	logger.Info("MySQL 连接成功 (Seeder)")

	// --- 4. 初始化 Kafka 生产者 ---
	kafkaProducer := producer.NewKafkaProducer(cfg.KafkaConfig, logger)
	logger.Info("Kafka 生产者已初始化 (Seeder)")

	// --- 5. 初始化 COS客户端 ---
	cos, cosError := dependencies.InitCOS(&cfg.COSConfig, logger)
	if cosError != nil {
		logger.Fatal("初始化 cos客户端 失败", zap.Error(cosError))
	}
	logger.Info("Redis 连接成功")

	// --- 6. 初始化 Repositories ---
	postRepo := mysql.NewPostRepository(db, logger)
	postDetailRepo := mysql.NewPostDetailRepository(db)
	postDetailImageRepo := mysql.NewPostDetailImageRepository(db)

	rdb, redisErr := dependencies.InitRedis(&cfg.RedisConfig, logger)
	if redisErr != nil {
		logger.Warn("初始化 Redis 失败 (Seeder)，部分依赖 Redis 的功能可能受限", zap.Error(redisErr))
	}
	var postViewRepo redisRepo.PostViewRepository
	if rdb != nil {
		postViewRepo = redisRepo.NewPostViewRepository(rdb, logger, 10000, 3, 0.01, cfg.ViewSyncConfig)
	} else {
		logger.Warn("PostViewRepository (Redis) 未初始化，依赖此仓库的功能将不可用")
	}

	// --- 7. 初始化 Service ---
	postSvc := postServicePkg.NewPostService(
		db,
		postRepo,
		postDetailRepo,
		postDetailImageRepo,
		cos,
		postViewRepo,
		kafkaProducer,
		logger,
	)
	logger.Info("PostService 已初始化 (Seeder)")

	// --- 8. 执行数据填充 ---
	ctx := context.Background()
	startTime := time.Now()
	logger.Info("开始执行数据填充...", zap.Int("预计数量", numPosts))

	Seed(ctx, postSvc, logger, numPosts)

	duration := time.Since(startTime)
	logger.Info("数据填充主要逻辑完成！", zap.Duration("耗时", duration)) // 修改日志消息

	// --- 9. 等待一段时间以确保异步 Kafka 任务有时间发送 ---
	if waitSeconds > 0 {
		logger.Info(fmt.Sprintf("Seeder: 数据填充请求已发送，等待 %d 秒以允许异步 Kafka 消息发送...", waitSeconds))
		time.Sleep(time.Duration(waitSeconds) * time.Second)
		logger.Info(fmt.Sprintf("Seeder: %d 秒等待结束。", waitSeconds))
	}

	fmt.Printf("数据填充完成！总耗时（包括等待）: %v\n", time.Since(startTime)) // 总耗时
	logger.Info("Seeder main: 所有任务完成（包括等待期），准备退出。")
}
