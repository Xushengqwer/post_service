package redis

import (
	"context"
	"errors" // 需要导入 errors 包
	"fmt"
	"strconv" // 需要导入 strconv 包
	"strings" // 需要导入 strings 包
	"time"

	"github.com/Xushengqwer/go-common/core" // 导入日志库
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap" // 导入 zap

	"github.com/Xushengqwer/post_service/constant"
)

// PostViewRepository 定义了与帖子浏览、排名相关的 Redis 操作接口。
// - 目标: 提供高性能的接口来处理帖子浏览计数（防刷）、获取热门帖子以及同步浏览量。
type PostViewRepository interface {
	// IncrementViewCount 原子性地增加指定帖子的浏览量，并更新其在热榜中的分数。
	// - 使用 Bloom Filter (`bloomKey`) 防止同一用户在短时间 (TTL) 内重复计数。
	// - 使用 Lua 脚本 (`luaScript`) 保证 Redis 中计数器 (`viewCountKey`) 和 ZSet (`hotPostsKey`) 的原子性更新。
	// - 输入: postID (帖子ID), userID (用于Bloom Filter的用户标识)。
	// - 输出: error 操作错误。如果用户已在 Bloom Filter 中，则返回 nil 且不执行计数增加。
	IncrementViewCount(ctx context.Context, postID uint64, userID string) error

	// GetTopHotPosts 从总排行榜 (`PostsRankKey`) 中获取浏览量最高的前 N 个帖子 ID。
	// - 主要用于定时任务，获取热门帖子的 ID 列表，以便后续缓存这些帖子的详细数据。
	// - 使用 ZRevRange 以分数（浏览量）降序获取成员（帖子ID）。
	// - 输入: n (需要获取的数量)。
	// - 输出: []uint64 (帖子 ID 列表), error 操作错误。
	GetTopHotPosts(ctx context.Context, n int) ([]uint64, error)

	// CreateHotList 原子性地从总排行榜 (`PostsRankKey`) 截取前 N 条记录，生成/覆盖热榜 (`HotPostsRankKey`)。
	// - 使用 Lua 脚本确保操作的原子性（读取、删除旧榜、写入新榜）。
	// - 目的是创建一个规模较小、更新频率可能更高的“当前热门”榜单。
	// - 输入: n (热榜的大小)。
	// - 输出: error 操作错误。
	CreateHotList(ctx context.Context, n int) error

	// GetAllViewCounts 使用 SCAN 命令分批获取 Redis 中所有帖子的浏览量计数。
	// - 目的是安全、高效地获取全量浏览量数据，作为同步到 MySQL 的数据源。
	// - 使用 SCAN 避免一次性 KEYS 操作阻塞 Redis，MGET 批量获取提高效率。
	// - 输入: ctx (上下文)。
	// - 输出: map[uint64]int64 (帖子 ID -> 浏览量), error 操作错误。
	GetAllViewCounts(ctx context.Context) (map[uint64]int64, error)
}

// postViewRepository 是 PostViewRepository 接口的 Redis 实现。
type postViewRepository struct {
	redisClient       *redis.Client   // Redis 客户端实例
	logger            *core.ZapLogger // 日志记录器实例
	bloomFilterSize   int64           // Bloom Filter 配置: 预期容量
	bloomFilterHashes uint            // Bloom Filter 配置: 哈希函数数量 (影响精度和空间)
	bloomErrorRate    float64         // Bloom Filter 配置: 可接受的误判率
}

// NewPostViewRepository 创建 PostViewRepository 实例。
// - 通过依赖注入传入 redisClient 和 logger。
// - Bloom Filter 相关参数也在此设置。
func NewPostViewRepository(redisClient *redis.Client, logger *core.ZapLogger, bloomFilterSize int64, bloomFilterHashes uint, bloomErrorRate float64) PostViewRepository { // 添加 logger 参数
	return &postViewRepository{
		redisClient:       redisClient,
		logger:            logger, // 初始化 logger
		bloomFilterSize:   bloomFilterSize,
		bloomFilterHashes: bloomFilterHashes,
		bloomErrorRate:    bloomErrorRate,
	}
}

// IncrementViewCount 实现增加帖子浏览量的逻辑。
func (r *postViewRepository) IncrementViewCount(ctx context.Context, postID uint64, userID string) error {
	// 构造相关的 Redis Key
	bloomKey := fmt.Sprintf("%s%d", constant.PostViewBloomPrefix, postID)     // Bloom Filter 的 Key，用于存储看过该帖子的用户
	viewCountKey := fmt.Sprintf("%s%d", constant.PostViewCountPrefix, postID) // 帖子浏览量计数器 Key (String 类型)
	postsRankKey := constant.PostsRankKey                                     // 全局帖子分数排行榜 Key (Sorted Set 类型)

	// 检查并可能初始化 Bloom Filter
	// BF.RESERVE 用于创建或调整过滤器，如果已存在且参数匹配则无操作。
	// 理论上 BF.EXISTS 检查可以省略，直接 BF.ADD，不存在会自动创建，但显式检查更清晰一点。
	exists, err := r.redisClient.Exists(ctx, bloomKey).Result()
	if err != nil {
		r.logger.Error("检查 Bloom Filter Key 是否存在失败", zap.Error(err), zap.String("bloomKey", bloomKey))
		return fmt.Errorf("检查 Bloom Filter Key '%s' 失败: %w", bloomKey, err)
	}
	if exists == 0 {
		// 如果 Bloom Filter 不存在，使用 BF.RESERVE 创建它。
		// 参数包括错误率和初始容量。
		if err := r.redisClient.BFReserve(ctx, bloomKey, r.bloomErrorRate, r.bloomFilterSize).Err(); err != nil {
			// 如果创建失败 (例如 Redis 出错)，则无法继续防刷逻辑，直接返回错误。
			r.logger.Error("创建 Bloom Filter 失败", zap.Error(err), zap.String("bloomKey", bloomKey))
			return fmt.Errorf("创建 Bloom Filter '%s' 失败: %w", bloomKey, err)
		}
		r.logger.Info("Bloom Filter 不存在，已创建", zap.String("bloomKey", bloomKey))
	}

	// 使用 Bloom Filter 判断用户是否已访问过 (可能存在误判，但概率由 errorRate 控制)。
	// BF.EXISTS 检查 userID 是否可能已存在于过滤器中。
	userExists, err := r.redisClient.BFExists(ctx, bloomKey, userID).Result()
	if err != nil {
		r.logger.Error("检查用户是否存在于 Bloom Filter 失败", zap.Error(err), zap.String("bloomKey", bloomKey), zap.String("userID", userID))
		return fmt.Errorf("检查 Bloom Filter 出错 ('%s', '%s'): %w", bloomKey, userID, err)
	}
	// 如果 BF.EXISTS 返回 true，表示用户 *可能* 访问过，我们选择不计数，直接返回 nil。
	if userExists {
		r.logger.Debug("用户已存在于 Bloom Filter，跳过计数", zap.String("bloomKey", bloomKey), zap.String("userID", userID), zap.Uint64("postID", postID))
		return nil
	}

	// 如果用户不存在于 Bloom Filter，将用户添加到过滤器中。
	// BF.ADD 操作。
	// 返回值是 bool 切片，这里我们不关心具体哪个 filter 添加成功，只关心整体操作是否出错。
	// 注意：如果并发很高，这里可能存在 race condition (两个请求都判断 userExists 为 false，然后都尝试 ADD)，
	// 但 Bloom Filter 本身设计上对重复 ADD 是幂等的，所以问题不大。
	// 更严谨可以考虑 Lua 脚本将 BF.EXISTS 和 BF.ADD 合并。
	_, err = r.redisClient.BFAdd(ctx, bloomKey, userID).Result() // 使用 BFAdd 替代 BFAddMulti
	if err != nil {
		r.logger.Error("添加用户到 Bloom Filter 失败", zap.Error(err), zap.String("bloomKey", bloomKey), zap.String("userID", userID))
		return fmt.Errorf("添加用户到 Bloom Filter '%s' 失败: %w", bloomKey, err)
	}

	// 首次添加用户后，为 Bloom Filter 设置过期时间，避免无限增长。
	// 这决定了用户防刷的时间窗口。
	if err := r.redisClient.Expire(ctx, bloomKey, constant.ViewTTL).Err(); err != nil {
		// 设置过期失败通常不严重影响核心功能，记录警告即可。
		r.logger.Warn("设置 Bloom Filter 过期时间失败", zap.Error(err), zap.String("bloomKey", bloomKey))
		// 不返回错误，继续执行计数
	}

	// 使用 Lua 脚本确保“增加浏览量计数”和“更新排行榜分数”这两个操作的原子性。
	// - KEYS[1]: 帖子的浏览量计数器 (String)
	// - KEYS[2]: 全局帖子排行榜 (Sorted Set)
	// - ARGV[1]: 帖子 ID (作为 Sorted Set 的成员)
	// 脚本逻辑：先 INCR 计数器，然后用 INCR 返回的新值作为分数更新 ZSet。
	luaScript := redis.NewScript(`
        local viewCount = redis.call("INCR", KEYS[1])
        redis.call("ZADD", KEYS[2], viewCount, ARGV[1])
        return viewCount
    `)

	// 执行 Lua 脚本。
	// 不太关心返回值 (新的 viewCount)，主要检查执行是否出错。
	_, err = luaScript.Run(ctx, r.redisClient, []string{viewCountKey, postsRankKey}, postID).Result()
	if err != nil {
		r.logger.Error("执行 Lua 脚本增加浏览量和更新排名失败", zap.Error(err), zap.Uint64("postID", postID))
		return fmt.Errorf("原子性增加浏览量失败 (PostID: %d): %w", postID, err)
	}

	r.logger.Debug("成功增加浏览量并更新排名", zap.Uint64("postID", postID))
	return nil
}

// GetTopHotPosts 实现获取前 N 热门帖子 ID 的逻辑。
func (r *postViewRepository) GetTopHotPosts(ctx context.Context, n int) ([]uint64, error) {
	// 使用 ZREVRANGE 从总排行榜 (按分数从高到低) 获取排名 0 到 n-1 的成员 (帖子ID)。
	// 返回的是字符串切片。
	postIDStrings, err := r.redisClient.ZRevRange(ctx, constant.PostsRankKey, 0, int64(n-1)).Result()
	if err != nil {
		// 处理 Redis 查询错误。
		if errors.Is(err, redis.Nil) { // 如果 ZSet 不存在或为空
			r.logger.Info("热门帖子排行榜 ZSet 为空或不存在", zap.String("key", constant.PostsRankKey))
			return []uint64{}, nil // 返回空列表，不视为错误
		}
		r.logger.Error("从 Redis ZRevRange 获取热门帖子 ID 失败", zap.Error(err), zap.String("key", constant.PostsRankKey), zap.Int("n", n))
		return nil, fmt.Errorf("获取前 %d 热门帖子失败: %w", n, err)
	}

	// 将获取到的字符串 ID 列表转换为 uint64 列表。
	ids := make([]uint64, 0, len(postIDStrings))
	for _, idStr := range postIDStrings {
		// 使用 strconv.ParseUint 将字符串转换为 uint64。
		// **修正：第三个参数 bitSize 应为 64 而不是 32。**
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			// 如果某个 ID 无法解析（理论上不应发生，除非 ZSet 中存了非数字），记录错误并可能需要决定是否中断。
			// 这里选择记录并跳过这个无法解析的 ID。
			r.logger.Error("解析热门帖子 ID 字符串失败", zap.Error(parseErr), zap.String("idStr", idStr))
			continue // 跳过这个错误的 ID
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// CreateHotList 实现生成热榜的逻辑。
func (r *postViewRepository) CreateHotList(ctx context.Context, n int) error {
	// 定义源 ZSet (总榜) 和目标 ZSet (热榜) 的 Key。
	fullRankKey := constant.PostsRankKey
	hotListKey := constant.HotPostsRankKey

	// 使用 Lua 脚本保证原子性：
	// 1. 从总榜 (`KEYS[1]`) 获取前 n (`ARGV[1]`) 条记录（带分数）。
	// 2. 删除旧的热榜 (`KEYS[2]`)，防止数据残留。
	// 3. 如果从总榜获取到了数据，使用 ZADD 将这些数据（成员和分数）写入新的热榜 (`KEYS[2]`)。
	// 脚本返回成功写入的帖子数量。
	luaScript := redis.NewScript(`
		local items = redis.call("ZRANGE", KEYS[1], 0, ARGV[1] - 1, "WITHSCORES")
		redis.call("DEL", KEYS[2])
		if #items > 0 then
			redis.call("ZADD", KEYS[2], unpack(items))
		end
		return #items / 2
	`)

	// 执行 Lua 脚本。
	_, err := luaScript.Run(ctx, r.redisClient, []string{fullRankKey, hotListKey}, n).Result()
	if err != nil {
		r.logger.Error("执行 Lua 脚本创建热榜失败", zap.Error(err), zap.Int("n", n))
		return fmt.Errorf("创建热榜 (Top %d) 失败: %w", n, err)
	}
	r.logger.Info("成功创建/更新热榜", zap.String("key", hotListKey), zap.Int("size", n))
	return nil
}

// GetAllViewCounts 实现获取所有帖子浏览量的逻辑。
func (r *postViewRepository) GetAllViewCounts(ctx context.Context) (map[uint64]int64, error) {
	// 初始化结果 map。
	viewCounts := make(map[uint64]int64)
	// SCAN 命令需要一个游标，初始为 0。
	var cursor uint64 = 0
	// 定义要扫描的 Key 模式。
	matchPattern := constant.PostViewCountPattern // 使用 'post_view_count:*' 模式
	// 定义每次 SCAN 返回的建议数量 (不是绝对保证)。
	scanCount := int64(constant.ScanBatchSize)

	r.logger.Info("开始扫描 Redis 获取所有帖子浏览量", zap.String("pattern", matchPattern))
	startTime := time.Now() // 记录开始时间

	// 使用 for 循环和 SCAN 命令迭代遍历所有匹配的 Key。
	// SCAN 是安全的，不会像 KEYS * 那样阻塞 Redis。
	for {
		// 执行 SCAN 命令。
		keys, nextCursor, err := r.redisClient.Scan(ctx, cursor, matchPattern, scanCount).Result()
		if err != nil {
			r.logger.Error("执行 Redis SCAN 命令失败", zap.Error(err), zap.Uint64("cursor", cursor), zap.String("pattern", matchPattern))
			return nil, fmt.Errorf("扫描 Redis Keys 失败 (模式: %s): %w", matchPattern, err)
		}

		// 如果当前批次扫描到 Key。
		if len(keys) > 0 {
			r.logger.Debug("SCAN 批次获取到 Keys", zap.Int("count", len(keys)), zap.Uint64("cursor", cursor))
			// 使用 MGET 批量获取这些 Key 的值。
			values, mgetErr := r.redisClient.MGet(ctx, keys...).Result()
			if mgetErr != nil {
				r.logger.Error("执行 Redis MGET 命令失败", zap.Error(mgetErr), zap.Int("keyCount", len(keys)))
				return nil, fmt.Errorf("批量获取浏览量值失败 (%d keys): %w", len(keys), mgetErr)
			}

			// 处理 MGET 返回的结果。
			// values 是一个 []interface{}，可能包含 nil (如果 key 不存在或类型不对)。
			for i, key := range keys {
				// 从 Key 中解析出 PostID。
				// 例如，"post_view_count:123" -> "123"
				postIDStr := strings.TrimPrefix(key, constant.PostViewCountPrefix)
				// **修正/优化：使用 ParseUint 更直接**
				postID, parseErr := strconv.ParseUint(postIDStr, 10, 64)
				if parseErr != nil {
					// 解析 PostID 失败，记录错误并可能跳过。
					r.logger.Error("从 Redis Key 解析 PostID 失败", zap.Error(parseErr), zap.String("key", key))
					continue // 跳过这个无法解析的 Key
				}

				// 解析浏览量值。
				viewCount := int64(0) // 默认值为 0
				if values[i] != nil { // 检查值是否存在
					if valueStr, ok := values[i].(string); ok && valueStr != "" { // 检查是否为非空字符串
						// 将字符串值解析为 int64。
						parsedCount, parseCountErr := strconv.ParseInt(valueStr, 10, 64)
						if parseCountErr != nil {
							// 解析浏览量失败，记录错误，但可能仍将此 PostID 记为 0。
							r.logger.Error("解析 Redis 中的浏览量值失败", zap.Error(parseCountErr), zap.String("key", key), zap.String("valueStr", valueStr))
							// 决定是否继续，这里我们选择继续，将计数视为 0
						} else {
							viewCount = parsedCount
						}
					} else {
						// 值不是预期的字符串类型，记录警告。
						r.logger.Warn("Redis Key 的值类型不是字符串或为空", zap.String("key", key), zap.Any("value", values[i]))
					}
				}
				// 将结果存入 map。
				viewCounts[postID] = viewCount
			}
		}

		// 更新游标，准备下一次 SCAN。
		cursor = nextCursor
		// 如果游标回到 0，表示迭代完成。
		if cursor == 0 {
			break
		}
	} // 结束 SCAN 循环

	duration := time.Since(startTime)
	r.logger.Info("完成扫描 Redis 帖子浏览量", zap.Int("totalCount", len(viewCounts)), zap.Duration("duration", duration))
	return viewCounts, nil
}
