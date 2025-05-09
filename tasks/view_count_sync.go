package tasks

import (
	"context"
	"time"

	"github.com/Xushengqwer/go-common/core" // 导入日志库
	"github.com/robfig/cron/v3"
	"go.uber.org/zap" // 导入 zap

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/repo/mysql"
	"github.com/Xushengqwer/post_service/repo/redis"
)

// SyncService 负责定时将 Redis 中的帖子浏览量同步到 MySQL 数据库。
// - 这是一个典型的后台数据同步任务，用于将缓存中的计数持久化。
type SyncService struct {
	postViewRepo redis.PostViewRepository // 依赖 Redis 仓库获取浏览量数据
	postRepo     mysql.PostRepository     // 依赖 MySQL 仓库执行批量更新
	cron         *cron.Cron               // cron V3 实例
	logger       *core.ZapLogger          // 新增：日志记录器
}

// NewSyncService 初始化并启动浏览量同步服务。
func NewSyncService(
	postViewRepo redis.PostViewRepository,
	postRepo mysql.PostRepository,
	logger *core.ZapLogger, // 新增 logger 参数
) *SyncService {
	cronV3 := cron.New() // 初始化 cron
	s := &SyncService{
		postViewRepo: postViewRepo,
		postRepo:     postRepo,
		cron:         cronV3,
		logger:       logger, // 初始化 logger
	}
	s.startCronJob() // 启动定时作业
	return s
}

// startCronJob 配置并启动 cron 作业。
// - 使用 constant.SyncInterval 定义的 cron 表达式来调度 syncViewCounts 方法。
func (s *SyncService) startCronJob() {
	schedule := constant.SyncViewCountInterval // 例如 "*/5 * * * *" 每 5 分钟
	s.logger.Info("准备启动浏览量同步定时任务", zap.String("schedule", schedule))

	// 添加定时执行的函数
	entryID, err := s.cron.AddFunc(schedule, func() {
		s.logger.Info("浏览量同步任务开始执行...")
		startTime := time.Now()
		// 为单次任务执行设置超时，例如 1 分钟
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		s.logger.Info("浏览量同步任务开始执行...")

		s.syncViewCounts(ctx) // 调用同步逻辑

		duration := time.Since(startTime)
		s.logger.Info("浏览量同步任务执行完毕", zap.Duration("duration", duration))
	})

	// 处理添加作业失败的情况
	if err != nil {
		s.logger.Fatal("添加浏览量同步 cron 作业失败", zap.Error(err), zap.String("schedule", schedule))
	}

	// 启动调度器
	s.cron.Start()
	s.logger.Info("浏览量同步定时任务已启动", zap.Uint("cronEntryID", uint(entryID)))
}

// syncViewCounts 是定时任务执行的实际同步逻辑。
// - 它从 Redis 获取全量的浏览量数据，然后调用 MySQL 仓库批量更新数据库。
func (s *SyncService) syncViewCounts(ctx context.Context) {
	// 1. 从 Redis 获取全量浏览量数据。
	//    GetAllViewCounts 内部使用 SCAN 和 MGET 实现。
	viewCounts, err := s.postViewRepo.GetAllViewCounts(ctx)
	if err != nil {
		// 获取数据失败，记录错误并中止本次同步。
		s.logger.Error("从 Redis 获取全量浏览量失败", zap.Error(err))
		return // 中止执行
	}
	countFromRedis := len(viewCounts)
	if countFromRedis == 0 {
		s.logger.Info("从 Redis 获取到的浏览量数据为空，无需同步")
		return // 没有数据需要同步
	}
	s.logger.Info("成功从 Redis 获取到浏览量数据", zap.Int("count", countFromRedis))

	// 2. 调用 MySQL 仓库批量更新数据库。
	//    BatchUpdateViewCount 内部实现分批次更新和错误处理（仅记录日志）。
	err = s.postRepo.BatchUpdateViewCount(ctx, viewCounts)
	// 注意：BatchUpdateViewCount 设计为总是返回 nil，内部错误通过日志记录。
	// 所以这里不需要检查 err。如果需要更严格的错误处理，需要修改 BatchUpdateViewCount 的行为。
	if err != nil {
		s.logger.Error("批量更新浏览量到 MySQL 失败", zap.Error(err))
	}

	// 记录同步完成信息。
	// 实际更新成功的数量取决于 BatchUpdateViewCount 内部各批次的执行情况。
	// 这里的日志只表示调用完成。需要结合仓库层的日志来判断具体成功/失败的批次数。
	s.logger.Info("调用 MySQL 批量更新浏览量操作完成", zap.Int("submittedCount", countFromRedis))
}

// Stop 优雅地停止 cron 调度器。
func (s *SyncService) Stop() context.Context { // 返回 context
	s.logger.Info("正在停止浏览量同步定时任务...")
	stopCtx := s.cron.Stop()
	s.logger.Info("浏览量同步定时任务已停止调度")
	return stopCtx
}

// 需要导入 "time" 包, zap 包
// import "time"
// import "go.uber.org/zap"
