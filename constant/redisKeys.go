package constant

// Redis Key 相关常量 (导出)
const (
	// --- Key 前缀 (用于动态生成 Key) ---

	// PostViewBloomPrefix 是帖子浏览记录 Bloom Filter 的 Key 前缀。
	// 每个帖子会有一个对应的 Bloom Filter Key。
	// 用于快速判断某个用户是否在一定时间内浏览过某帖子，以实现防刷。
	// 示例 Key: "post_view_bloom:123" (其中 123 是 postID)
	// Redis 类型: String (由 RedisBloom 模块管理)
	PostViewBloomPrefix = "post_view_bloom:"

	// PostViewCountPrefix 是帖子浏览量计数器的 Key 前缀。
	// 每个帖子会有一个对应的 String 类型的 Key，用于原子性计数。
	// 示例 Key: "post_view_count:123" (其中 123 是 postID)
	// Redis 类型: String
	// 示例值: "58" (表示帖子 123 的浏览量为 58)
	PostViewCountPrefix = "post_view_count:"

	// PostsHashKey 是一个示例性的 Hash Key 名称。
	// 注意: "posts" 这个名称比较通用，请根据实际用途确认其是否合适或添加更具体的描述。
	// Hash Key 通常用于存储一个对象的多个字段。
	// 示例 Key (如果按 postID 存储): "posts:hash:123"
	// Redis 类型: Hash
	// 示例字段与值 (对于 "posts:hash:123"): Field="title", Value="帖子标题"; Field="author_id", Value="789"
	PostsHashKey = "posts"

	// PostDetailCacheKeyPrefix 是帖子详情缓存的 Key 前缀。
	// 每个帖子详情会有一个对应的 Key。
	// 示例 Key: "post_detail:123" (其中 123 是 postID)
	// Redis 类型: String (通常存储 JSON 或其他序列化格式的数据)
	// 示例值: "{\"title\":\"帖子标题\",\"content\":\"帖子内容...\"}"
	PostDetailCacheKeyPrefix = "post_detail:"

	// --- 固定 Key 名称 (全局使用的 Key) ---

	// PostsRankKey 是全局帖子排行榜的 Key 名称。
	// 这是一个 Sorted Set (ZSet)，成员是帖子 ID (postID)，分数是浏览量 (viewCount)。
	// 用于存储所有帖子的实时排名。
	// Redis 类型: Sorted Set
	// 示例成员与分数: Member="123", Score=58; Member="456", Score=102
	PostsRankKey = "post_rank"

	// HotPostsRankKey 是热门帖子榜单的 Key 名称。
	// 这通常是一个较小的 Sorted Set (ZSet)，由定时任务从 PostsRankKey 中截取 Top N 生成。
	// 用于快速获取当前最热门的帖子列表。
	// Redis 类型: Sorted Set
	// 示例成员与分数: (与 PostsRankKey 类似，但通常条目较少)
	HotPostsRankKey = "hot_post_rank"
)
