package config

// ViewSyncConfig 包含浏览量同步任务相关的配置
type ViewSyncConfig struct {
	// BatchSize 是将 Redis 中的浏览量同步到 MySQL 数据库时，每个数据库操作批次处理的帖子数量。
	// 例如，如果从 Redis 获取到 200,000 条帖子的浏览量需要同步，且 BatchSize 设置为 500，
	// 则 BatchUpdatePostViewCounts 方法会将这 200,000 条数据分割成 200,000 / 500 = 400 个小批次。
	// 每个小批次包含 500 条帖子的更新数据，将通过一次数据库 UPDATE 操作（例如使用 CASE WHEN 语句）完成。
	// 这个参数主要影响单次数据库 UPDATE 语句的复杂度和处理的数据行数。
	BatchSize int `mapstructure:"batchSize" json:"batchSize" yaml:"batchSize"`

	// ConcurrencyLevel 是执行浏览量同步到 MySQL 任务时，并发处理数据批次的 worker (goroutine) 数量。
	// 接上例，如果有 400 个数据批次（每批 500 条）需要处理，且 ConcurrencyLevel 设置为 4，
	// 则系统会启动 4 个 worker goroutine。这 4 个 worker 会并行地从任务队列中获取不同的小批次进行处理，
	// 每个 worker 独立执行其批次的数据库更新操作。
	// 这个参数主要影响同时向数据库发起更新请求的并发连接数。
	ConcurrencyLevel int `mapstructure:"concurrencyLevel" json:"concurrencyLevel" yaml:"concurrencyLevel"`

	// ScanBatchSize 是从 Redis 使用 SCAN 命令获取所有帖子浏览量 Key 时，
	// 传递给 SCAN 命令的 COUNT 参数的建议值。
	// 这表示每次 SCAN 调用期望 Redis 返回大约多少个 Key。
	// Redis 不保证精确返回此数量，但会以此为提示。
	// 较大的值可能会减少 SCAN 的迭代次数，但单次操作可能稍慢；较小的值则相反。
	// 例如，如果设置为 1000，则 GetAllViewCounts 方法每次会尝试从 Redis 获取约 1000 个匹配的 Key。
	ScanBatchSize int64 `mapstructure:"scanBatchSize" json:"scanBatchSize" yaml:"scanBatchSize"`
}
