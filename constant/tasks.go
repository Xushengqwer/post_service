package constant

import "time"

// 定时任务调度表达式 (Cron Spec)
const (
	HotPostsCacheCronSpec = "@every 15m"   // 热帖缓存刷新频率
	SyncViewCountInterval = "*/10 * * * *" // 浏览量同步频率 (每 120 分钟)
	// SyncViewCountInterval = "@every 2h"  // 等效的写法
)

// 批处理大小 (Batch Sizes)
const (
	SyncBatchSize   = 500  // 同步浏览量到 MySQL 的批次大小
	BatchDetailSize = 100  // 批量加载帖子详情的批次大小
	ScanBatchSize   = 1000 // Redis SCAN 命令的建议 count 值
)

const (
	MaxRetryTimes = 3 // 缓存或数据库操作的最大重试次数
)

// 可以添加其他与任务相关的常量...
// 例如，任务超时时间
const (
	HotPostsCacheTaskTimeout = 5 * time.Minute
	SyncViewCountTaskTimeout = 1 * time.Minute
)

const (
	// ViewSyncConcurrency 是浏览量同步任务中并发更新 MySQL 的 Goroutine 数量。
	// 设置为 1 表示顺序执行。大于 1 表示并发执行。
	// 需要确保 MySQL 连接池的 MaxOpenConns 至少不小于此值。
	ViewSyncConcurrency = 2 // 默认为 2
)
