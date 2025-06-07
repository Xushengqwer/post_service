// File: tasks/hot_posts_cache.go
package tasks

import (
	"context"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/repo/redis" // 假设 PostTaskCache 接口定义在此
)

// HotPostsCacheTask 负责定时刷新 Redis 中的热门帖子缓存。
// 它协调生成热榜快照，并基于该快照更新帖子基本信息Hash和帖子详情缓存。
type HotPostsCacheTask struct {
	taskCache redis.PostTaskCache // 修改：依赖新的 PostTaskCache 接口
	cron      *cron.Cron
	logger    *core.ZapLogger
}

// NewHotPostsCacheTask 初始化并启动热门帖子缓存的定时任务。
// - taskCache: 实现了 redis.PostTaskCache 接口的实例。
// - logger: ZapLogger 实例。
func NewHotPostsCacheTask(taskCache redis.PostTaskCache, logger *core.ZapLogger) *HotPostsCacheTask {
	cronV3 := cron.New() // 默认分钟级精度

	task := &HotPostsCacheTask{
		taskCache: taskCache, // 修改：使用 taskCache
		cron:      cronV3,
		logger:    logger,
	}
	task.startCronJob()
	return task
}

// startCronJob 配置并启动 cron 作业。
func (t *HotPostsCacheTask) startCronJob() {
	schedule := constant.HotPostsCacheCronSpec
	t.logger.Info("准备启动热门帖子相关缓存刷新定时任务", zap.String("schedule", schedule))

	entryID, err := t.cron.AddFunc(schedule, func() {
		t.logger.Info("热门帖子相关缓存刷新任务开始执行...")
		startTime := time.Now()
		// 为单次任务执行设置超时，例如 10 分钟，以防止任务卡死。
		// 超时时间的设定应大于各子任务（CreateHotList, CacheHotPostsToRedis, CacheHotPostDetailsToRedis）
		// 正常执行时间的总和，并留有一定余量。
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		t.syncHotCaches(ctx) // 调用新的同步逻辑

		duration := time.Since(startTime)
		t.logger.Info("热门帖子相关缓存刷新任务执行完毕", zap.Duration("duration", duration))
	})

	if err != nil {
		t.logger.Fatal("添加热门帖子相关缓存刷新 cron 作业失败", zap.Error(err), zap.String("schedule", schedule))
	}

	t.cron.Start()
	t.logger.Info("热门帖子相关缓存刷新定时任务已启动", zap.Uint("cronEntryID", uint(entryID)))
}

// syncHotCaches 是定时任务执行的实际同步逻辑。
// 它按顺序调用 PostTaskCache 接口的方法：
// 1. 创建/更新热榜快照 (ZSet)。
// 2. 基于快照同步热门帖子基本信息到 Hash。
// 3. 基于快照同步热门帖子详情到独立的 String Key。
func (t *HotPostsCacheTask) syncHotCaches(ctx context.Context) {
	// 步骤 1: 创建/更新热榜快照 (constant.HotPostsRankKey)
	// 这个快照将作为后续两个缓存更新步骤的数据源。
	t.logger.Info("任务步骤1: 开始创建/更新热榜快照 ZSet...")
	if err := t.taskCache.CreateHotList(ctx, constant.HotPostsCacheSize); err != nil {
		// 如果创建热榜快照失败，后续的缓存更新可能基于旧的或不一致的数据源，
		// 或者如果热榜 ZSet 不存在，后续步骤会失败。
		// 这是一个关键步骤，其失败应被高度关注。
		t.logger.Error("创建/更新热榜快照 ZSet 失败，后续缓存可能不准确", zap.Error(err))
		// 决定是否中止：如果此步失败，后续步骤意义不大，可以考虑直接返回。
		// return // 或者允许继续，但后续步骤很可能会因为源数据问题而部分或全部失败。
		// 当前选择：记录错误并继续，让后续步骤自行处理源数据问题。
	} else {
		t.logger.Info("任务步骤1: 成功创建/更新热榜快照 ZSet")
	}

	// 步骤 2: 同步热门帖子基本信息到 Hash 缓存。
	// 此方法现在依赖于步骤 1 生成的 `constant.HotPostsRankKey`。
	t.logger.Info("任务步骤2: 开始同步热门帖子基本信息到 Redis Hash...")
	if err := t.taskCache.CacheHotPostsToRedis(ctx); err != nil {
		t.logger.Error("同步热门帖子基本信息到 Redis Hash 失败", zap.Error(err))
		// 记录错误，但允许任务继续到下一步。
	} else {
		t.logger.Info("任务步骤2: 成功同步热门帖子基本信息到 Redis Hash")
	}

	// 步骤 3: 同步热门帖子详情到独立的 Redis Key。
	// 此方法也依赖于步骤 1 生成的 `constant.HotPostsRankKey` 作为热门ID的来源。
	t.logger.Info("任务步骤3: 开始同步热门帖子详情到 Redis...")
	if err := t.taskCache.CacheHotPostDetailsToRedis(ctx); err != nil {
		t.logger.Error("同步热门帖子详情到 Redis 失败", zap.Error(err))
		// 记录错误。
	} else {
		t.logger.Info("任务步骤3: 成功同步热门帖子详情到 Redis")
	}
}

// Stop 优雅地停止 cron 调度器。
func (t *HotPostsCacheTask) Stop() context.Context {
	t.logger.Info("正在停止热门帖子相关缓存刷新定时任务...")
	stopCtx := t.cron.Stop()
	t.logger.Info("热门帖子相关缓存刷新定时任务已停止调度。等待正在执行的任务完成...")
	return stopCtx // 调用者可以使用此 context 等待任务结束
}
