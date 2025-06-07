package constant

import "time"

// Bloom Filter 默认参数常量
const (
	BloomFilterDefaultSize      int64   = 100000 // 预期插入数量
	BloomFilterDefaultErrorRate float64 = 0.01   // 期望误判率
	BloomFilterDefaultHashes    uint    = 7      // 哈希函数数量
	// BloomViewTTL 定义了 Bloom Filter (用于浏览防刷) 的过期时间 (Time-To-Live)。
	// 这个时间窗口决定了在多长时间内，同一用户的浏览只被计数一次。
	BloomViewTTL time.Duration = 12 * time.Hour
)
