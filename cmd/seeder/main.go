package main

import (
	"flag"
	"log"

	sharedCore "github.com/Xushengqwer/go-common/core"
	appConfig "github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/dependencies"
	"go.uber.org/zap"
)

// 目的：批量在MySQL插入假数据作为测试数据
func main() {
	// 可以保留 flag 来指定数量，或者直接硬编码
	configFile := flag.String("config", "config/config.development.yaml", "配置文件路径")
	seedCount := flag.Int("count", 1000, "要填充的帖子数量")
	flag.Parse()

	// 1. 加载配置
	var cfg appConfig.PostConfig
	if err := sharedCore.LoadConfig(*configFile, &cfg); err != nil {
		log.Fatalf("FATAL: 加载配置失败 (%s): %v", *configFile, err)
	}

	// 2. 初始化 Logger 和 DB (只需要这两样)
	logger, loggerErr := sharedCore.NewZapLogger(cfg.ZapConfig)
	if loggerErr != nil {
		log.Fatalf("FATAL: 初始化 ZapLogger 失败: %v", loggerErr)
	}
	db, dbErr := dependencies.InitMySQL(&cfg, logger)
	if dbErr != nil {
		logger.Fatal("初始化 MySQL 数据库失败", zap.Error(dbErr))
	}
	logger.Info("数据库连接成功，准备执行 Seeder...")

	// 3. 调用 Seeder 逻辑
	if *seedCount <= 0 {
		logger.Fatal("-count 必须大于 0")
	}
	if err := RunSeeder(db, logger, *seedCount); err != nil {
		logger.Fatal("数据库填充失败", zap.Error(err))
	}

	logger.Info("数据库填充成功完成！")
	// Seeder 执行完自动退出
}
