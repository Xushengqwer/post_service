package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/myErrors"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"strconv"

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/models/entities"
	"github.com/Xushengqwer/post_service/repo/mysql"
)

// Cache 定义了帖子相关的缓存操作接口。
// - 目标: 提供 Redis 缓存层，加速热点数据的访问，减轻数据库压力。
// - 包括: 热榜帖子列表缓存、帖子详情缓存、排名查询等。
type Cache interface {
	// GetPostRank 获取指定帖子在热榜 ZSet (`HotPostsRankKey`) 中的排名（0-based, 降序）。
	// - 返回 -1 表示帖子不在榜单中。
	GetPostRank(ctx context.Context, postID uint64) (int64, error)

	// GetPostsByRange 从热榜 ZSet (`HotPostsRankKey`) 获取指定排名范围内的帖子 ID 列表。
	// - 用于分页加载热门帖子列表。
	// - start, stop 是基于 0 的排名索引。
	GetPostsByRange(ctx context.Context, start, stop int64) ([]uint64, error)

	// GetPosts 从 Redis Hash (`PostsHashKey`) 中批量获取帖子实体。
	// - 根据帖子 ID 列表，高效获取缓存的帖子信息，用于信息流等场景。
	// - 返回的帖子实体中 ViewCount 反映的是缓存刷新时的快照值。
	GetPosts(ctx context.Context, postIDs []uint64) ([]*entities.Post, error)

	// GetPostDetail 从 Redis (`PostDetailCacheKeyPrefix:{id}` key) 获取单个帖子详情。
	// - 用于访问热点帖子的详情页。
	// - 如果缓存未命中，返回 myerrors.ErrCacheMiss，上层服务需要处理回源。
	GetPostDetail(ctx context.Context, postID uint64) (*vo.PostDetailVO, error)
}

// cacheImpl 是 Cache 接口的 Redis 实现。
type cacheImpl struct {
	postViewRepo PostViewRepository                  // 依赖 PostView 仓库获取排名/ID
	postBatch    mysql.PostBatchOperationsRepository // 依赖postBatch仓库
	redisClient  *redis.Client                       // Redis 客户端实例
	logger       *core.ZapLogger                     // 日志记录器实例
}

// NewCache 是 cacheImpl 的构造函数。
// - 通过依赖注入初始化所有必需的组件。
func NewCache(
	postViewRepo PostViewRepository,
	postBatch mysql.PostBatchOperationsRepository,
	redisClient *redis.Client,
	logger *core.ZapLogger, // 添加 logger 参数
) Cache {
	return &cacheImpl{
		postViewRepo: postViewRepo,
		postBatch:    postBatch,
		redisClient:  redisClient,
		logger:       logger, // 初始化 logger
	}
}

// GetPostRank 实现获取帖子排名。
// 排名是 0-based，分数越高，排名越靠前 (即 ZREVRANK 的结果)。
func (c *cacheImpl) GetPostRank(ctx context.Context, postID uint64) (int64, error) {
	// 1. 确定要操作的 Redis Key 和 成员 (Member)
	// 使用 constant.HotPostsRankKey 作为热榜的 Sorted Set Key。
	key := constant.HotPostsRankKey
	// Sorted Set 中的成员通常存储为字符串。
	member := fmt.Sprintf("%d", postID)

	c.logger.Debug("开始从 Redis 获取帖子排名",
		zap.String("key", key),
		zap.String("member_postID", member),
	)

	// 2. 执行 ZREVRANK 命令
	// ZREVRANK 返回成员在 Sorted Set 中的排名，按分数从高到低排序。
	// 如果成员不存在，命令会返回一个错误 (redis.Nil)。
	rank, err := c.redisClient.ZRevRank(ctx, key, member).Result()

	// 3. 处理命令执行结果
	if err != nil {
		// 3a. 检查错误是否为 redis.Nil (表示成员不存在于 ZSet 中)
		if errors.Is(err, redis.Nil) {
			c.logger.Info("帖子不在热榜 ZSet 中 (或 ZSet 本身不存在)",
				zap.Uint64("postID", postID),
				zap.String("key", key),
			)
			// 按照接口约定，返回 -1 表示帖子不在榜单中，此时操作本身没有发生 Redis 通信错误。
			return -1, nil
		}
		// 3b. 处理其他类型的 Redis 错误 (例如连接问题、服务器错误等)
		c.logger.Error("从 Redis 获取帖子排名失败",
			zap.Error(err),
			zap.Uint64("postID", postID),
			zap.String("key", key),
		)
		// 返回 -1 和具体的错误信息。调用者应检查 error 是否为 nil。
		return -1, fmt.Errorf("获取帖子(ID: %d)在热榜(key: %s)中的排名失败: %w", postID, key, err)
	}

	// 4. ZREVRANK 成功执行，返回获取到的排名 (0-based)
	c.logger.Debug("成功从 Redis 获取帖子排名",
		zap.String("key", key),
		zap.String("member_postID", member),
		zap.Int64("rank", rank),
	)
	return rank, nil
}

// GetPostsByRange 实现按排名范围获取帖子 ID。
// start 和 stop 是 0-based 的排名索引，按分数从高到低排列。
func (c *cacheImpl) GetPostsByRange(ctx context.Context, start, stop int64) ([]uint64, error) {
	// 1. 确定要操作的 Redis Key。
	key := constant.HotPostsRankKey // 使用热榜 Key。

	c.logger.Debug("开始从 Redis 按排名范围获取帖子 ID",
		zap.String("key", key),
		zap.Int64("start_rank", start),
		zap.Int64("stop_rank", stop),
	)

	// 2. 参数校验：确保 start 和 stop 是有效的范围。
	// Redis 的 ZREVRANGE 对于 start > stop 或者 start/stop 超出 ZSet 大小的情况有其自身的处理方式
	// (通常返回空列表)，但我们可以在客户端进行一些基本校验以避免无效查询或意外行为。
	if start < 0 {
		// start 不应为负。如果为负，Redis ZREVRANGE 会从尾部开始计算，可能不是期望行为。为简化，我们这里将其视为无效参数。
		c.logger.Warn("GetPostsByRange: start 参数为负数，视为无效请求，返回空列表。",
			zap.Int64("start", start),
			zap.Int64("stop", stop),
		)
		return []uint64{}, nil
	}
	if start > stop && stop != -1 { // stop 为 -1 表示到 ZSet 末尾，此时 start > stop 是可能的（如果 start 很大）
		// 如果 start > stop (且 stop 不是 -1)，这是一个无效的范围，ZREVRANGE 会返回空。
		c.logger.Info("GetPostsByRange: start 排名大于 stop 排名，这是一个无效范围，返回空列表。",
			zap.Int64("start", start),
			zap.Int64("stop", stop),
			zap.String("key", key),
		)
		return []uint64{}, nil
	}

	// 3. 执行 ZREVRANGE 命令。
	// ZREVRANGE key start stop [WITHSCORES]
	// 返回指定排名范围内的成员 (字符串形式的 ID)。
	idStrs, err := c.redisClient.ZRevRange(ctx, key, start, stop).Result()

	// 4. 处理命令执行结果。
	if err != nil {
		// 4a. 检查错误是否为 redis.Nil。
		// 对于 ZREVRANGE，如果 Key 不存在，或者请求的范围超出了 ZSet 的实际大小，
		// 它通常会返回一个空列表，并且 err 为 redis.Nil。
		if errors.Is(err, redis.Nil) {
			c.logger.Info("按排名范围获取帖子 ID：热榜 ZSet 为空/不存在，或请求的范围超出实际大小，返回空列表。",
				zap.Int64("start", start),
				zap.Int64("stop", stop),
				zap.String("key", key),
			)
			return []uint64{}, nil // 返回空列表，不视为操作性错误。
		}
		// 4b. 处理其他类型的 Redis 错误。
		c.logger.Error("从 Redis ZRevRange 按排名范围获取帖子 ID 失败",
			zap.Error(err),
			zap.Int64("start", start),
			zap.Int64("stop", stop),
			zap.String("key", key),
		)
		return nil, fmt.Errorf("获取排名 %d-%d 的帖子 ID 失败 (key: %s): %w", start, stop, key, err)
	}

	// 4c. 如果 ZREVRANGE 成功执行 (err == nil) 但返回的 idStrs 为空。
	// 这通常意味着 ZSet 存在，但请求的范围是空的或超出了实际元素。
	if len(idStrs) == 0 {
		c.logger.Info("按排名范围获取帖子 ID：热榜 ZSet 存在，但请求的范围 [start: %d, stop: %d] 内没有数据，返回空列表。",
			zap.Int64("start", start),
			zap.Int64("stop", stop),
			zap.String("key", key),
		)
		return []uint64{}, nil
	}

	// 5. 将获取到的字符串 ID 列表转换为 uint64 列表。
	ids := make([]uint64, 0, len(idStrs))
	parseErrors := 0
	for _, idStr := range idStrs {
		// strconv.ParseUint: base=10, bitSize=64 (用于 uint64)。
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			// 如果从 ZSet 中取出的成员无法解析为 uint64 (理论上不应发生，除非 ZSet 数据被污染)，
			// 记录错误并跳过此 ID，以保证其他有效 ID 仍能被处理。
			c.logger.Warn("解析 ZSet 中的帖子 ID 字符串失败，已跳过该 ID。",
				zap.Error(parseErr),
				zap.String("idStr", idStr),
				zap.String("rankKey", key),
			)
			parseErrors++
			continue // 跳过这个无法解析的 ID
		}
		ids = append(ids, id)
	}

	if parseErrors > 0 {
		c.logger.Warn("在转换从 ZSet 获取的帖子 ID 时，部分 ID 解析失败。",
			zap.Int("totalFromZSet", len(idStrs)),
			zap.Int("parseErrors", parseErrors),
			zap.Int("successfulConversions", len(ids)),
		)
	}

	c.logger.Debug("成功从 Redis 按排名范围获取帖子 ID 列表。",
		zap.String("key", key),
		zap.Int64("start_rank", start),
		zap.Int64("stop_rank", stop),
		zap.Int("returned_id_count", len(ids)),
	)
	return ids, nil
}

// GetPosts 从 Redis Hash (`PostsHashKey`) 中批量获取帖子实体。
// - 根据帖子 ID 列表，高效获取缓存的帖子信息。
// - 返回的帖子实体中 ViewCount 反映的是 CacheHotPostsToRedis 任务缓存刷新时的快照值。
func (c *cacheImpl) GetPosts(ctx context.Context, postIDs []uint64) ([]*entities.Post, error) {
	// 1. 处理边界情况：如果请求的 ID 列表为空，则直接返回空列表。
	if len(postIDs) == 0 {
		c.logger.Debug("GetPosts: 请求的 postIDs 列表为空，返回空帖子列表。")
		return []*entities.Post{}, nil
	}

	// 2. 准备 HMGET 命令所需的参数。
	//    - hashKey: 存储帖子缓存的 Redis Hash 的键名。
	//    - fields: 需要从 Hash 中获取的字段列表，即字符串形式的 postID。
	hashKey := constant.PostsHashKey // 与 CacheHotPostsToRedis 中使用的键一致
	fields := make([]string, len(postIDs))
	for i, id := range postIDs {
		fields[i] = fmt.Sprintf("%d", id)
	}

	c.logger.Debug("开始从 Redis Hash 批量获取帖子",
		zap.String("hashKey", hashKey),
		zap.Int("requested_id_count", len(postIDs)),
		// zap.Strings("fields_to_get", fields), // 记录 fields 可能会很长，酌情开启
	)

	// 3. 执行 HMGET 命令批量获取数据。
	// HMGET 返回一个 []interface{}，其顺序与请求的 fields 顺序一致。
	// 如果某个 field 在 Hash 中不存在，则结果列表中对应位置的值为 nil。
	values, err := c.redisClient.HMGet(ctx, hashKey, fields...).Result()
	if err != nil {
		// 如果 HMGET 命令本身失败（例如 Redis 连接问题），则记录错误并返回。
		c.logger.Error("从 Redis Hash 批量获取帖子失败 (HMGET 执行错误)",
			zap.Error(err),
			zap.String("hashKey", hashKey),
			zap.Int("idCount", len(postIDs)),
		)
		return nil, fmt.Errorf("批量获取帖子缓存 (key: %s) 失败: %w", hashKey, err)
	}

	// 4. 处理 HMGET 返回的结果，反序列化 JSON 数据。
	posts := make([]*entities.Post, 0, len(postIDs)) // 预估容量，最多为请求的 ID 数量
	cacheMissCount := 0                              // 记录缓存未命中的数量
	unmarshalErrorCount := 0                         // 记录反序列化失败的数量

	for i, val := range values {
		requestedPostID := postIDs[i] // 当前处理的原始 postID (uint64)

		// 4a. 检查 HMGET 返回的值是否为 nil，表示该 postID 在缓存中未找到 (cache miss)。
		if val == nil {
			cacheMissCount++
			c.logger.Debug("帖子 Hash 缓存未命中",
				zap.Uint64("postID", requestedPostID),
				zap.String("hashKey", hashKey),
				zap.String("field", fields[i]),
			)
			continue // 跳过未命中的 ID
		}

		// 4b. 尝试将获取到的值断言为字符串（因为我们存的是 JSON 字符串）。
		jsonStr, ok := val.(string)
		if !ok {
			// 如果值不是字符串，这是一个异常情况，可能表示缓存数据被意外修改。
			unmarshalErrorCount++
			c.logger.Error("帖子 Hash 缓存中的值类型不是预期的字符串，跳过该帖子",
				zap.Uint64("postID", requestedPostID),
				zap.String("hashKey", hashKey),
				zap.String("field", fields[i]),
				zap.Any("valueType", fmt.Sprintf("%T", val)),
			)
			continue
		}

		// 4c. 反序列化 JSON 字符串到 entities.Post 结构体。
		var post entities.Post
		if jsonErr := json.Unmarshal([]byte(jsonStr), &post); jsonErr != nil {
			// 如果 JSON 反序列化失败，可能表示缓存数据已损坏。
			unmarshalErrorCount++
			c.logger.Error("反序列化帖子 Hash 缓存数据失败，跳过该帖子",
				zap.Error(jsonErr),
				zap.Uint64("postID", requestedPostID),
				zap.String("hashKey", hashKey),
				zap.String("field", fields[i]),
				// zap.String("json_string_to_parse", jsonStr), // 记录原始JSON可能过长，酌情
			)
			// 考虑：是否需要删除这个损坏的缓存条目？ c.redisClient.HDel(ctx, hashKey, fields[i])
			continue
		}

		// 4d. 反序列化成功，将帖子添加到结果列表中。
		posts = append(posts, &post)
	}

	// 5. 记录操作总结日志并返回结果。
	c.logger.Debug("批量获取帖子 Hash 缓存完成",
		zap.String("hashKey", hashKey),
		zap.Int("requested_id_count", len(postIDs)),
		zap.Int("found_in_cache_count", len(posts)),
		zap.Int("cache_miss_count", cacheMissCount),
		zap.Int("unmarshal_error_count", unmarshalErrorCount),
	)
	return posts, nil
}

// GetPostDetail 从 Redis 获取单个帖子详情 (vo.PostDetailResponse)。
// - 如果缓存未命中，返回 myerrors.ErrCacheMiss，上层服务应处理回源。
// - 如果缓存数据损坏或发生其他 Redis 错误，则返回相应的错误。
func (c *cacheImpl) GetPostDetail(ctx context.Context, postID uint64) (*vo.PostDetailVO, error) {
	// 1. 构造缓存 Key。
	//    Key 的格式应与 CacheHotPostDetailsToRedis 方法中写入时使用的最终 Key 格式一致。
	//    例如："post_detail:<postID>"
	key := fmt.Sprintf("%s%d", constant.PostDetailCacheKeyPrefix, postID) // 使用 Sprintf 更安全
	c.logger.Debug("尝试从 Redis 获取帖子详情 VO", zap.String("key", key), zap.Uint64("postID", postID))

	// 2. 执行 GET 命令从 Redis 获取序列化后的数据 (应为 JSON 字符串)。
	jsonData, err := c.redisClient.Get(ctx, key).Result()

	// 3. 处理 Redis GET 命令的执行结果。
	if err != nil {
		// 3a. 如果错误是 redis.Nil，表示 Key 不存在，即缓存未命中。
		if errors.Is(err, redis.Nil) {
			c.logger.Info("帖子详情 VO 缓存未命中", zap.String("key", key), zap.Uint64("postID", postID))
			// 返回应用层定义的缓存未命中错误，上层服务应处理回源逻辑。
			return nil, myErrors.ErrCacheMiss
		}
		// 3b. 如果是其他类型的 Redis 错误 (例如连接问题、服务器错误等)。
		c.logger.Error("从 Redis 获取帖子详情 VO 失败 (GET 命令执行错误)",
			zap.Error(err),
			zap.String("key", key),
			zap.Uint64("postID", postID),
		)
		return nil, fmt.Errorf("获取帖子(ID: %d)详情缓存 (key: %s) 失败: %w", postID, key, err)
	}

	// 4. 反序列化 JSON 数据到 *vo.PostDetailVO 结构体。
	//    此操作依赖于 CacheHotPostDetailsToRedis 方法缓存的是 vo.PostDetailVO 的 JSON 序列化形式。
	var postDetailVO vo.PostDetailVO
	if jsonErr := json.Unmarshal([]byte(jsonData), &postDetailVO); jsonErr != nil {
		// 如果 JSON 反序列化失败，表明缓存数据可能已损坏。
		c.logger.Error("反序列化帖子详情 VO 缓存数据失败",
			zap.Error(jsonErr),
			zap.String("key", key),
			zap.Uint64("postID", postID),
			// zap.String("jsonData_preview", jsonData[:min(len(jsonData), 200)]), // 记录部分JSON预览
		)
		// 考虑：主动删除这个损坏的 Key 以避免后续请求也读到坏数据。
		// if delErr := c.redisClient.Del(context.Background(), key).Err(); delErr != nil {
		//    c.logger.Error("删除损坏的帖子详情缓存失败", zap.Error(delErr), zap.String("key", key))
		// }
		return nil, fmt.Errorf("解析帖子(ID: %d)详情缓存 (key: %s) 数据失败: %w", postID, key, jsonErr)
	}

	// 5. 成功获取并反序列化，返回帖子详情 VO。
	c.logger.Debug("成功从 Redis 获取并解析帖子详情 VO", zap.String("key", key), zap.Uint64("postID", postID))
	return &postDetailVO, nil
}
