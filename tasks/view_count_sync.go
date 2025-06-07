package tasks

import (
	"context"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/repo/mysql" // 确保导入的是包含 PostBatchOperationsRepository 的包
	"github.com/Xushengqwer/post_service/repo/redis"
)

// ViewCountSyncTask 负责定时将 Redis 中的帖子浏览量同步到 MySQL 数据库。
// (原 SyncService 重命名为 ViewCountSyncTask 以更清晰地表达其职责)
type ViewCountSyncTask struct {
	postViewRepo  redis.PostViewRepository            // Redis 仓库，用于获取浏览量
	postBatchRepo mysql.PostBatchOperationsRepository // MySQL 批量操作仓库，用于更新浏览量
	cron          *cron.Cron                          // cron V3 实例
	logger        *core.ZapLogger                     // 日志记录器
}

// NewViewCountSyncTask 初始化并启动浏览量同步的定时任务。
func NewViewCountSyncTask(
	postViewRepo redis.PostViewRepository,
	postBatchRepo mysql.PostBatchOperationsRepository, // 修改依赖为 PostBatchOperationsRepository
	logger *core.ZapLogger,
) *ViewCountSyncTask {
	cronV3 := cron.New() // 默认分钟级精度
	task := &ViewCountSyncTask{
		postViewRepo:  postViewRepo,
		postBatchRepo: postBatchRepo, // 修改赋值
		cron:          cronV3,
		logger:        logger,
	}
	task.startCronJob() // 在构造函数中启动定时作业
	return task
}

// startCronJob 配置并启动 cron 作业。
// 使用 constant.SyncViewCountInterval 定义的 cron 表达式来调度 syncViewCountsToDB 方法。
func (t *ViewCountSyncTask) startCronJob() {
	schedule := constant.SyncViewCountInterval
	t.logger.Info("准备启动帖子浏览量同步MySQL定时任务", zap.String("schedule", schedule))

	entryID, err := t.cron.AddFunc(schedule, func() {
		t.logger.Info("帖子浏览量同步MySQL任务开始执行...")
		startTime := time.Now()
		// 为单次任务执行设置超时，例如 3 分钟。
		// 这个超时应该足够完成 Redis 数据获取和 MySQL 批量更新。
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		t.syncViewCountsToDB(ctx) // 调用核心同步逻辑

		duration := time.Since(startTime)
		t.logger.Info("帖子浏览量同步MySQL任务执行完毕", zap.Duration("duration", duration))
	})

	if err != nil {
		// 如果添加 cron 作业失败（通常是 schedule 表达式错误），记录致命错误。
		t.logger.Fatal("添加帖子浏览量同步 cron 作业失败", zap.Error(err), zap.String("schedule", schedule))
	}

	t.cron.Start() // 启动 cron 调度器 (在后台 goroutine 中运行)
	t.logger.Info("帖子浏览量同步MySQL定时任务已启动", zap.Uint("cronEntryID", uint(entryID)))
}

// syncViewCountsToDB 是定时任务执行的实际同步逻辑。
// 1. 从 Redis 获取全量的帖子浏览量数据。
// 2. 调用 MySQL 仓库的 BatchUpdatePostViewCount 方法批量更新到数据库。
func (t *ViewCountSyncTask) syncViewCountsToDB(ctx context.Context) {
	t.logger.Info("任务步骤1: 开始从 Redis 获取全量帖子浏览量...")
	// 调用 PostViewRepository 的 GetAllViewCounts 方法
	viewCounts, err := t.postViewRepo.GetAllViewCounts(ctx)
	if err != nil {
		// 如果从 Redis 获取数据失败，记录错误并中止本次同步。
		t.logger.Error("从 Redis 获取全量浏览量失败，本次同步中止。", zap.Error(err))
		return
	}

	countFromRedis := len(viewCounts)
	if countFromRedis == 0 {
		t.logger.Info("从 Redis 获取到的浏览量数据为空，无需同步到 MySQL。")
		return // 没有数据需要同步
	}
	t.logger.Info("任务步骤1: 成功从 Redis 获取到浏览量数据。", zap.Int("帖子数量", countFromRedis))

	t.logger.Info("任务步骤2: 开始将浏览量批量更新到 MySQL...")
	// 调用 PostBatchOperationsRepository 的 BatchUpdatePostViewCount 方法
	// 注意：根据之前的设计，BatchUpdatePostViewCount 内部处理错误并记录日志，通常返回 nil。
	// 如果 BatchUpdatePostViewCount 的设计更改为会返回关键错误，则这里的错误处理需要调整。
	if err := t.postBatchRepo.BatchUpdatePostViewCounts(ctx, viewCounts); err != nil {
		// 尽管 BatchUpdatePostViewCount 可能设计为不向上层返回错误（仅记录日志），
		// 但以防万一其行为改变或返回了未预期的错误，这里还是加上错误记录。
		t.logger.Error("调用 MySQL 批量更新浏览量操作时发生意外错误（BatchUpdatePostViewCount 本应内部处理）",
			zap.Error(err),
			zap.Int("提交数量", countFromRedis),
		)
	} else {
		// 这里的日志表示调用已完成。实际的成功/失败情况需查看 BatchUpdatePostViewCount 的内部日志。
		t.logger.Info("任务步骤2: 调用 MySQL 批量更新浏览量操作已完成。", zap.Int("提交数量", countFromRedis))
	}
}

// Stop 优雅地停止 cron 调度器。
// 返回一个 context，调用者可以使用它来等待正在运行的任务完成。
func (t *ViewCountSyncTask) Stop() context.Context {
	t.logger.Info("正在停止帖子浏览量同步MySQL定时任务...")
	stopCtx := t.cron.Stop() // cron.Stop() 停止新任务调度，并返回一个在其管理的任务都完成后关闭的 context
	t.logger.Info("帖子浏览量同步MySQL定时任务已停止调度。等待正在执行的任务完成...")
	return stopCtx
}
