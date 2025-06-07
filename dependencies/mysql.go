// dependencies/mysql.go
package dependencies

import (
	"database/sql"
	"fmt"
	"gorm.io/plugin/dbresolver"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	appConfig "github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/models/entities"
)

// InitMySQL 初始化 MySQL 连接，并配置读写分离 (如果配置了从库)
func InitMySQL(cfg *appConfig.PostConfig, logger *core.ZapLogger) (*gorm.DB, error) {
	mysqlCfg := cfg.MySQLConfig     // 获取 MySQL 配置
	gormLogCfg := cfg.GormLogConfig // 获取 GORM 日志配置

	// --- 主库连接 ---
	if mysqlCfg.Write.DSN == "" {
		return nil, fmt.Errorf("主数据库 DSN (mysql.write.dsn) 未配置")
	}
	gormLogger := core.NewGormLogger(logger, gormLogCfg)
	gormConfig := &gorm.Config{
		Logger: gormLogger,
	}

	var db *gorm.DB
	var err error
	maxRetries := 5
	retryInterval := 2 * time.Second

	// 重试连接主库
	logger.Info("开始连接主数据库...")
	for i := 0; i < maxRetries; i++ {
		db, err = gorm.Open(mysql.Open(mysqlCfg.Write.DSN), gormConfig)
		if err == nil {
			var sqlDB *sql.DB
			var dbErr error
			sqlDB, dbErr = db.DB()
			if dbErr == nil {
				pingErr := sqlDB.Ping()
				if pingErr == nil {
					err = nil // 连接和 Ping 都成功
					break
				}
				err = pingErr // Ping 失败
			} else {
				err = dbErr // 获取 sql.DB 失败
			}
		}
		logger.Warn("无法连接到主数据库，尝试重试", zap.Int("retry", i+1), zap.Int("maxRetries", maxRetries), zap.Error(err))
		if i < maxRetries-1 {
			time.Sleep(retryInterval)
		}
	}
	if err != nil {
		logger.Error("无法连接到主数据库", zap.Error(err))
		return nil, fmt.Errorf("无法连接到主数据库: %w", err)
	}
	logger.Info("成功连接到主数据库")

	// --- 配置读写分离 (dbresolver) ---
	readReplicas := make([]gorm.Dialector, 0, len(mysqlCfg.Read))
	for i, replicaCfg := range mysqlCfg.Read {
		if replicaCfg.DSN == "" {
			logger.Warn("发现空的从库 DSN 配置，已跳过", zap.Int("index", i))
			continue
		}
		readReplicas = append(readReplicas, mysql.Open(replicaCfg.DSN))
		logger.Info("发现并准备配置从数据库", zap.Int("index", i))
	}

	// 只有在配置了有效的从库时才启用读写分离插件
	if len(readReplicas) > 0 {
		resolverConfig := dbresolver.Config{
			Sources:  []gorm.Dialector{mysql.Open(mysqlCfg.Write.DSN)}, // 主库作为写源
			Replicas: readReplicas,                                     // 从库作为读源
			Policy:   dbresolver.StrictRoundRobinPolicy(),              // 使用轮询策略分配读请求
		}
		err = db.Use(dbresolver.Register(resolverConfig))
		if err != nil {
			logger.Error("配置 GORM 读写分离插件失败", zap.Error(err))
			return nil, fmt.Errorf("配置 GORM 读写分离失败: %w", err)
		}
		logger.Info("成功配置 GORM 读写分离插件", zap.Int("从库数量", len(readReplicas)))
	} else {
		logger.Info("未配置有效的从数据库，不启用读写分离")
	}

	// --- 配置连接池 ---
	sqlDB, dbErr := db.DB()
	if dbErr != nil {
		logger.Error("无法获取数据库对象以配置连接池", zap.Error(dbErr))
		return nil, fmt.Errorf("无法获取数据库对象: %w", dbErr)
	}

	// 应用连接池设置 (以共享设置为基础，允许被 Write/Read 的独立设置覆盖，这里简化处理，直接用共享设置)
	// 注意：更复杂的逻辑可以检查 mysqlCfg.Write.MaxOpenConns 等是否为 nil 来决定是否覆盖
	maxIdle := mysqlCfg.SharedMaxIdleConns
	maxOpen := mysqlCfg.SharedMaxOpenConns
	maxLife := mysqlCfg.SharedConnMaxLifetime

	// 检查主库是否覆盖共享设置
	if mysqlCfg.Write.MaxIdleConns != nil {
		maxIdle = *mysqlCfg.Write.MaxIdleConns
	}
	if mysqlCfg.Write.MaxOpenConns != nil {
		maxOpen = *mysqlCfg.Write.MaxOpenConns
	}
	if mysqlCfg.Write.ConnMaxLifetime != nil {
		maxLife = *mysqlCfg.Write.ConnMaxLifetime
	}
	// 注意：如果启用了读写分离，理论上所有连接（读和写）都使用同一个池。
	// 如果需要为读写设置不同的池参数，dbresolver 可能不支持，需要更复杂的设置。
	// 通常设置一个足够大的共享池即可。

	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetConnMaxLifetime(time.Duration(maxLife) * time.Second)

	logger.Info("配置数据库连接池",
		zap.Int("最大空闲连接数", maxIdle),
		zap.Int("最大打开连接数", maxOpen),
		zap.Int("连接最大生命周期(秒)", maxLife),
	)
	// 再次 Ping 确保配置后连接池可用
	if pingErr := sqlDB.Ping(); pingErr != nil {
		logger.Error("配置连接池后 Ping 数据库失败", zap.Error(pingErr))
		return nil, fmt.Errorf("配置连接池后 Ping 失败: %w", pingErr)
	}

	// --- 自动迁移 ---
	// AutoMigrate 默认会发送到主库 (Source)
	logger.Info("开始执行数据库自动迁移...")
	migrateErr := db.AutoMigrate(
		&entities.Post{},
		&entities.PostDetail{},
		&entities.PostDetailImage{},
		// ... 其他需要迁移的实体 ...
	)
	if migrateErr != nil {
		logger.Error("数据库自动迁移失败", zap.Error(migrateErr))
		return nil, fmt.Errorf("数据库自动迁移失败: %w", migrateErr)
	}
	logger.Info("数据库自动迁移完成")

	logger.Info("成功初始化 MySQL 连接 (包括读写分离和自动迁移)")
	return db, nil
}
