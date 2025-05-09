package constant

// Bloom Filter 默认参数常量
const (
	BloomFilterDefaultSize      int64   = 100000 // 预期插入数量
	BloomFilterDefaultErrorRate float64 = 0.01   // 期望误判率
	BloomFilterDefaultHashes    uint    = 7      // 哈希函数数量
)
