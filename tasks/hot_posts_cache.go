package tasks

import (
	"context"
	"time" // 建议为 syncHotPosts 添加超时

	"github.com/Xushengqwer/go-common/core" // 导入日志库
	"github.com/robfig/cron/v3"
	"go.uber.org/zap" // 导入 zap

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/repo/redis"
)

// HotPostsCacheTask 负责定时刷新 Redis 中的热门帖子缓存。
// - 包括热门帖子列表 Hash (`posts` key) 和热门帖子详情缓存 (`post_detail:{id}` keys)。
// - 目的是将热点数据预加载到 Redis，提高前端访问性能，降低数据库压力。
type HotPostsCacheTask struct {
	cache  redis.Cache     // 依赖 Cache 接口执行具体的缓存刷新操作
	cron   *cron.Cron      // cron V3 实例，用于任务调度
	logger *core.ZapLogger // 新增：日志记录器
}

// NewHotPostsCacheTask 初始化并启动热门帖子缓存的定时任务。
// - cache: 实现了 redis.Cache 接口的实例。
// - logger: ZapLogger 实例。
// - 返回: *HotPostsCacheTask 实例。
func NewHotPostsCacheTask(cache redis.Cache, logger *core.ZapLogger) *HotPostsCacheTask { // 添加 logger 参数
	// 使用秒级精度初始化 cron 调度器（如果需要的话）
	// cronV3 := cron.New(cron.WithSeconds())
	// 默认是分钟级精度
	cronV3 := cron.New()

	task := &HotPostsCacheTask{
		cache:  cache,
		cron:   cronV3,
		logger: logger, // 初始化 logger
	}
	// 在构造函数中直接启动定时作业
	task.startCronJob()
	return task
}

// startCronJob 配置并启动 cron 作业。
// - 使用 constant.HotPostsCacheCronSpec 定义的 cron 表达式来调度 syncHotPosts 方法。
func (t *HotPostsCacheTask) startCronJob() {
	schedule := constant.HotPostsCacheCronSpec // 例如 "@every 15m"
	t.logger.Info("准备启动热门帖子缓存刷新定时任务", zap.String("schedule", schedule))

	// 使用 cron 库添加定时函数。
	entryID, err := t.cron.AddFunc(schedule, func() {
		// 使用匿名函数包装，可以方便地添加日志或 panic 恢复
		t.logger.Info("热门帖子缓存刷新任务开始执行...")
		startTime := time.Now()
		// 为单次任务执行设置超时，防止任务卡死影响下一次调度
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // 例如，设置 5 分钟超时
		defer cancel()

		t.syncHotPosts(ctx) // 调用实际的同步逻辑

		duration := time.Since(startTime)
		t.logger.Info("热门帖子缓存刷新任务执行完毕", zap.Duration("duration", duration))
	})

	// 如果添加作业失败（通常是 cron 表达式格式错误），则记录致命错误并退出。
	// 在初始化阶段失败是可接受的。
	if err != nil {
		t.logger.Fatal("添加热门帖子缓存刷新 cron 作业失败", zap.Error(err), zap.String("schedule", schedule))
		// 注意: Fatal 会导致程序退出 os.Exit(1)
	}

	// 启动 cron 调度器（在后台 goroutine 中运行）
	t.cron.Start()
	t.logger.Info("热门帖子缓存刷新定时任务已启动", zap.Uint("cronEntryID", uint(entryID))) // entryID 类型是 cron.EntryID (int)
}

// syncHotPosts 是定时任务执行的实际同步逻辑。
// - 它按顺序调用 Cache 接口的方法来刷新帖子列表缓存和帖子详情缓存。
// - 使用传入的带超时的 context。
func (t *HotPostsCacheTask) syncHotPosts(ctx context.Context) {
	// 1. 同步热门帖子列表到 Hash 缓存。
	//    这个方法负责生成 ZSet 热榜快照，并根据快照更新 Hash 缓存。
	if err := t.cache.CacheHotPostsToRedis(ctx); err != nil {
		// 记录错误，但通常不应让一个子任务的失败中断整个刷新流程。
		t.logger.Error("同步热门帖子列表到 Redis Hash 失败", zap.Error(err))
		// 可以考虑添加监控告警
	} else {
		t.logger.Info("成功同步热门帖子列表到 Redis Hash")
	}

	// 2. 同步热门帖子详情到独立的 Redis Key。
	//    这个方法负责获取热榜 ID，清理旧缓存，从 MySQL 获取详情，写入 Redis 并设置 TTL。
	if err := t.cache.CacheHotPostDetailsToRedis(ctx); err != nil {
		// 同样，记录错误并继续。
		t.logger.Error("同步热门帖子详情到 Redis 失败", zap.Error(err))
		// 可以考虑添加监控告警
	} else {
		t.logger.Info("成功同步热门帖子详情到 Redis")
	}
}

// Stop 优雅地停止 cron 调度器。
// - 在应用关闭时调用，以停止新的任务调度并等待当前正在执行的任务完成（如果任务本身支持）。
// - cron.Stop() 返回一个 context，可用于等待正在运行的任务结束。
func (t *HotPostsCacheTask) Stop() context.Context { // 返回 context 以便调用者等待
	t.logger.Info("正在停止热门帖子缓存刷新定时任务...")
	stopCtx := t.cron.Stop() // cron.Stop() 会停止调度新任务，并返回一个在其管理的任务都完成后关闭的 context
	t.logger.Info("热门帖子缓存刷新定时任务已停止调度")
	return stopCtx
}
