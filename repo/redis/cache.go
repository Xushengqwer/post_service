package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xushengqwer/post_service/myErrors"
	"strconv"
	"strings"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/models/entities"
	"github.com/Xushengqwer/post_service/repo/mysql"
)

// Cache 定义了帖子相关的缓存操作接口。
// - 目标: 提供 Redis 缓存层，加速热点数据的访问，减轻数据库压力。
// - 包括: 热榜帖子列表缓存、帖子详情缓存、排名查询等。
type Cache interface {
	// CacheHotPostsToRedis 将数据库中的热门帖子（基于 ZSet 排名）全量同步到 Redis Hash (`PostsHashKey`) 中。
	// - 定时任务调用，采用全量刷新策略保证 Hash 数据与 ZSet 快照的一致性。
	// - 负责用最新的 TopN 热帖数据覆盖 Hash 缓存。
	// - 会用 ZSet 的分数快照更新帖子实体的 ViewCount 后再存入 Hash。
	CacheHotPostsToRedis(ctx context.Context) error

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

	// CacheHotPostDetailsToRedis 将热榜帖子的 *详情* 数据从 MySQL 同步到 Redis (单独的 `PostDetailCacheKeyPrefix:{id}` keys)。
	// - 定时任务调用，为热点帖子详情提供缓存。
	// - 获取热榜 TopN 的 ID，清理不再热门的详情，分批从 MySQL 查询新详情，写入 Redis 并设置 TTL。
	// - 包含重试和错误处理机制。
	CacheHotPostDetailsToRedis(ctx context.Context) error

	// GetPostDetail 从 Redis (`PostDetailCacheKeyPrefix:{id}` key) 获取单个帖子详情。
	// - 用于访问热点帖子的详情页。
	// - 如果缓存未命中，返回 myerrors.ErrCacheMiss，上层服务需要处理回源。
	GetPostDetail(ctx context.Context, postID uint64) (*entities.PostDetail, error)
}

// cacheImpl 是 Cache 接口的 Redis 实现。
type cacheImpl struct {
	postViewRepo   PostViewRepository         // 依赖 PostView 仓库获取排名/ID
	postRepo       mysql.PostRepository       // 依赖 Post 仓库从 MySQL 获取数据
	postDetailRepo mysql.PostDetailRepository // 依赖 PostDetail 仓库从 MySQL 获取数据
	redisClient    *redis.Client              // Redis 客户端实例
	logger         *core.ZapLogger            // 日志记录器实例
}

// NewCache 是 cacheImpl 的构造函数。
// - 通过依赖注入初始化所有必需的组件。
func NewCache(
	postViewRepo PostViewRepository,
	postRepo mysql.PostRepository,
	postDetailRepo mysql.PostDetailRepository,
	redisClient *redis.Client,
	logger *core.ZapLogger, // 添加 logger 参数
) Cache {
	return &cacheImpl{
		postViewRepo:   postViewRepo,
		postRepo:       postRepo,
		postDetailRepo: postDetailRepo,
		redisClient:    redisClient,
		logger:         logger, // 初始化 logger
	}
}

// CacheHotPostsToRedis 将热门帖子缓存到 Redis Hash (新方案：全量获取并覆盖)
func (c *cacheImpl) CacheHotPostsToRedis(ctx context.Context) error {
	startTime := time.Now()
	c.logger.Info("开始同步热门帖子到 Redis Hash (全量覆盖方案)")

	// 1. 确保最新的热榜 ZSet (`HotPostsRankKey`) 已生成
	if err := c.postViewRepo.CreateHotList(ctx, constant.HotPostsCacheSize); err != nil {
		c.logger.Error("生成热榜 ZSet 失败", zap.Error(err))
		return fmt.Errorf("生成热榜失败: %w", err)
	}
	c.logger.Debug("已生成/更新热榜 ZSet", zap.String("zsetKey", constant.HotPostsRankKey))

	// 2. 从热榜 ZSet 获取 TopN 帖子 ID 和分数 (浏览量快照)
	hotListKey := constant.HotPostsRankKey
	postScores, err := c.redisClient.ZRevRangeWithScores(ctx, hotListKey, 0, int64(constant.HotPostsCacheSize-1)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			c.logger.Info("热榜 ZSet 为空，清空帖子 Hash 缓存")
			// 如果热榜为空，最好也清空旧的 Hash 缓存
			if delErr := c.redisClient.Del(ctx, constant.PostsHashKey).Err(); delErr != nil {
				c.logger.Error("清空帖子 Hash 缓存失败", zap.Error(delErr), zap.String("key", constant.PostsHashKey))
				// 即使清空失败，也认为此次操作完成（可能是 Redis 暂时问题）
			}
			return nil
		}
		c.logger.Error("从热榜 ZSet 获取帖子分数失败", zap.Error(err), zap.String("key", hotListKey))
		return fmt.Errorf("获取热榜 ZSet 失败: %w", err)
	}

	// 3. 解析 ZSet 结果，得到 ID 列表 (uint64) 和 ID (string) -> Score 的映射
	currentHotPostIDs := make([]uint64, 0, len(postScores))
	currentScoreMap := make(map[string]float64)
	for _, z := range postScores {
		idStr := z.Member.(string)
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			c.logger.Warn("解析热榜 ZSet 成员 ID 失败，跳过", zap.Error(parseErr), zap.String("member", idStr))
			continue
		}
		currentHotPostIDs = append(currentHotPostIDs, id)
		currentScoreMap[idStr] = z.Score
	}

	// 如果解析后列表为空（可能所有成员都解析失败？），也认为无需后续操作
	if len(currentHotPostIDs) == 0 {
		c.logger.Warn("热榜 ZSet 有成员，但未能成功解析任何 PostID")
		// 同样可以考虑清空 Hash 缓存
		c.redisClient.Del(ctx, constant.PostsHashKey) // 忽略错误
		return nil
	}
	c.logger.Debug("解析热榜 ZSet 完成", zap.Int("hotCount", len(currentHotPostIDs)))

	// 4. 根据热榜 ID 列表，从 MySQL 批量获取帖子实体数据
	//    这里会读取 TopN 条记录，是潜在的数据库压力点。
	postsFromDB, dbErr := c.postRepo.GetPostsByIDs(ctx, currentHotPostIDs)
	if dbErr != nil {
		c.logger.Error("从 MySQL 批量获取热门帖子失败", zap.Error(dbErr), zap.Int("idCount", len(currentHotPostIDs)))
		return fmt.Errorf("获取帖子数据失败: %w", dbErr)
	}
	// 数据库可能返回比请求 ID 数量少的记录（如果某些 ID 不存在或被删除）
	c.logger.Debug("成功从 MySQL 获取热门帖子数据", zap.Int("fetchedCount", len(postsFromDB)))

	// 5. 准备要写入 Redis Hash 的数据 (map[string]interface{})
	//    Key 是帖子 ID (string)，Value 是序列化后的帖子实体 JSON 字符串。
	//    同时，用 ZSet 的分数快照覆盖从数据库读出的 ViewCount。
	dataToCache := make(map[string]interface{})
	marshalErrors := 0
	foundInDBSet := make(map[uint64]bool) // 记录在数据库中找到的 ID

	for _, post := range postsFromDB {
		foundInDBSet[post.ID] = true // 标记此 ID 在数据库中找到了
		idStr := fmt.Sprintf("%d", post.ID)

		// 使用 ZSet 快照分数覆盖数据库中的浏览量
		if score, exists := currentScoreMap[idStr]; exists {
			post.ViewCount = int64(score)
		} else {
			// 如果一个在 DB 中存在的帖子 ID 不在 ZSet 快照里，说明 ZSet 可能在此期间发生了变化，记录警告
			c.logger.Warn("数据库返回的帖子 ID 不在 ZSet 快照分数 Map 中？", zap.Uint64("postID", post.ID))
			// 此时 post.ViewCount 保持数据库的值
		}

		// 序列化更新后的帖子实体
		jsonData, jsonErr := json.Marshal(post)
		if jsonErr != nil {
			c.logger.Error("序列化帖子实体失败，跳过该帖子", zap.Error(jsonErr), zap.Uint64("postID", post.ID))
			marshalErrors++
			continue
		}
		// 将 ID(string) 和 JSON 数据放入待缓存 map
		dataToCache[idStr] = jsonData
	}

	// 检查是否有 ZSet 中的 ID 在数据库中未找到（可能已被删除）
	if len(postsFromDB) < len(currentHotPostIDs) {
		for _, hotID := range currentHotPostIDs {
			if !foundInDBSet[hotID] {
				c.logger.Warn("热榜中的 PostID 在数据库中未找到，无法缓存", zap.Uint64("postID", hotID))
			}
		}
	}

	// 6. 将数据写入 Redis Hash
	hashKey := constant.PostsHashKey
	if len(dataToCache) > 0 {
		// 方案 A: 使用 HMSet 一次性写入 (如果 go-redis 支持 map[string]interface{})
		// if err := c.redisClient.HMSet(ctx, hashKey, dataToCache).Err(); err != nil { ... }

		// 方案 B: 使用 Pipeline HSet (更通用)
		pipe := c.redisClient.Pipeline()
		// (可选，但推荐) 先删除旧的 Hash Key，确保完全覆盖
		pipe.Del(ctx, hashKey)
		// 再 HSet 所有新数据
		for field, value := range dataToCache {
			pipe.HSet(ctx, hashKey, field, value)
		}
		_, execErr := pipe.Exec(ctx)
		if execErr != nil {
			c.logger.Error("执行 Redis Pipeline 写入帖子 Hash 失败", zap.Error(execErr))
			return fmt.Errorf("写入帖子 Hash 缓存失败: %w", execErr)
		}
		c.logger.Info("成功写入帖子到 Redis Hash", zap.Int("cachedCount", len(dataToCache)), zap.Int("marshalErrors", marshalErrors))

	} else {
		// 如果 ZSet 不为空，但从 DB 没获取到任何数据或全部序列化失败，也应清空 Hash。
		c.logger.Warn("没有准备好任何数据写入帖子 Hash 缓存，清空旧缓存", zap.Int("hotIDs", len(currentHotPostIDs)), zap.Int("dbPosts", len(postsFromDB)), zap.Int("marshalErrors", marshalErrors))
		if delErr := c.redisClient.Del(ctx, hashKey).Err(); delErr != nil {
			c.logger.Error("清空帖子 Hash 缓存失败", zap.Error(delErr), zap.String("key", hashKey))
		}
	}

	duration := time.Since(startTime)
	c.logger.Info("完成同步热门帖子到 Redis Hash (全量覆盖方案)", zap.Duration("duration", duration))
	return nil
}

// GetPostRank 实现获取帖子排名。
func (c *cacheImpl) GetPostRank(ctx context.Context, postID uint64) (int64, error) {
	// ZREVRANK 返回的是 0-based 排名，分数最高的是 0。
	key := constant.HotPostsRankKey     // 使用热榜 Key
	member := fmt.Sprintf("%d", postID) // ZSet 成员是字符串

	rank, err := c.redisClient.ZRevRank(ctx, key, member).Result()
	if err != nil {
		// 如果错误是 redis.Nil，表示成员不存在于 ZSet 中。
		if errors.Is(err, redis.Nil) {
			c.logger.Debug("帖子不在热榜 ZSet 中", zap.Uint64("postID", postID), zap.String("key", key))
			return -1, nil // 返回 -1 表示不存在
		}
		// 其他 Redis 错误。
		c.logger.Error("获取帖子 ZSet 排名失败", zap.Error(err), zap.Uint64("postID", postID), zap.String("key", key))
		return -1, fmt.Errorf("获取帖子(ID: %d)排名失败: %w", postID, err)
	}
	// ZRevRank 成功，返回排名。
	return rank, nil
}

// GetPostsByRange 实现按排名范围获取帖子 ID。
func (c *cacheImpl) GetPostsByRange(ctx context.Context, start, stop int64) ([]uint64, error) {
	key := constant.HotPostsRankKey // 使用热榜 Key

	// 使用 ZREVRANGE 获取指定排名范围内的成员（字符串形式的 ID）。
	idStrs, err := c.redisClient.ZRevRange(ctx, key, start, stop).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) { // 范围超出或 ZSet 为空
			c.logger.Info("按排名范围获取帖子 ID：范围无效或 ZSet 为空", zap.Int64("start", start), zap.Int64("stop", stop), zap.String("key", key))
			return []uint64{}, nil // 返回空列表
		}
		c.logger.Error("按排名范围获取帖子 ID 失败", zap.Error(err), zap.Int64("start", start), zap.Int64("stop", stop), zap.String("key", key))
		return nil, fmt.Errorf("获取排名 %d-%d 的帖子 ID 失败: %w", start, stop, err)
	}

	// 将字符串 ID 列表转换为 uint64 列表。
	ids := make([]uint64, 0, len(idStrs))
	for _, idStr := range idStrs {
		// **修正：bitSize 应为 64**
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			c.logger.Warn("解析 ZSet 中的帖子 ID 失败，跳过", zap.Error(parseErr), zap.String("idStr", idStr))
			continue // 跳过无法解析的 ID
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetPosts 实现批量从 Hash 获取帖子实体。
func (c *cacheImpl) GetPosts(ctx context.Context, postIDs []uint64) ([]*entities.Post, error) {
	// 处理空 ID 列表的情况。
	if len(postIDs) == 0 {
		return []*entities.Post{}, nil
	}

	// 将 uint64 ID 列表转换为 HMGET 需要的字符串 field 列表。
	keys := make([]string, len(postIDs))
	for i, id := range postIDs {
		keys[i] = fmt.Sprintf("%d", id)
	}
	hashKey := constant.PostsHashKey // 帖子 Hash 缓存 Key

	// 使用 HMGET 批量获取 Hash 中对应 field 的 value (JSON 字符串)。
	// 返回值是 []interface{}，每个元素对应一个 key，如果 key 不存在则为 nil。
	values, err := c.redisClient.HMGet(ctx, hashKey, keys...).Result()
	if err != nil {
		c.logger.Error("从 Redis Hash 批量获取帖子失败", zap.Error(err), zap.String("key", hashKey), zap.Int("idCount", len(postIDs)))
		return nil, fmt.Errorf("批量获取帖子缓存失败: %w", err)
	}

	// 反序列化获取到的 JSON 数据。
	posts := make([]*entities.Post, 0, len(postIDs)) // 预估容量
	missCount := 0                                   // 记录缓存未命中数
	unmarshalErrors := 0                             // 记录反序列化失败数

	for i, val := range values {
		// 检查 HMGET 返回的值是否为 nil (表示缓存未命中)。
		if val == nil {
			missCount++
			c.logger.Debug("帖子 Hash 缓存未命中", zap.Uint64("postID", postIDs[i]), zap.String("key", hashKey))
			continue // 跳过未命中的 ID
		}

		// 尝试将值断言为字符串并反序列化。
		if jsonStr, ok := val.(string); ok {
			var post entities.Post
			if jsonErr := json.Unmarshal([]byte(jsonStr), &post); jsonErr != nil {
				unmarshalErrors++
				c.logger.Error("反序列化帖子 Hash 缓存数据失败", zap.Error(jsonErr), zap.Uint64("postID", postIDs[i]), zap.String("key", hashKey))
				continue // 跳过反序列化失败的数据
			}
			posts = append(posts, &post)
		} else {
			// 如果值不是字符串（异常情况）
			unmarshalErrors++
			c.logger.Warn("帖子 Hash 缓存中的值类型不是字符串", zap.Uint64("postID", postIDs[i]), zap.String("key", hashKey), zap.Any("valueType", fmt.Sprintf("%T", val)))
			continue
		}
	}
	c.logger.Debug("批量获取帖子 Hash 缓存完成", zap.Int("requested", len(postIDs)), zap.Int("found", len(posts)), zap.Int("misses", missCount), zap.Int("errors", unmarshalErrors))
	return posts, nil
}

// CacheHotPostDetailsToRedis 实现缓存热门帖子详情的逻辑。
func (c *cacheImpl) CacheHotPostDetailsToRedis(ctx context.Context) error {
	startTime := time.Now()
	c.logger.Info("开始同步热门帖子详情到 Redis")

	// 1. 从 PostView 仓库获取当前热榜 TopN 的帖子 ID。
	postIDs, err := c.postViewRepo.GetTopHotPosts(ctx, constant.HotPostsCacheSize)
	if err != nil {
		c.logger.Error("从 Redis 获取热门帖子 ID 失败", zap.Error(err))
		return fmt.Errorf("获取热门帖子 ID 失败: %w", err)
	}

	// 如果没有热门帖子，则无需进行后续操作。
	if len(postIDs) == 0 {
		c.logger.Info("热门帖子 ID 列表为空，无需缓存详情")
		return nil
	}
	c.logger.Debug("获取到热门帖子 ID", zap.Int("count", len(postIDs)))

	// 2. 清理 Redis 中可能已过期的（不再热门的）帖子详情缓存。
	//    这是为了避免旧的、不再热门的详情数据无限期地留在缓存中。
	if err := c.cleanExpiredPostDetails(ctx, postIDs); err != nil {
		// 清理失败通常不应阻塞主同步流程，记录警告。
		c.logger.Warn("清理过期帖子详情缓存失败", zap.Error(err))
		// 继续执行后续步骤
	}

	// 3. 分批处理这些热门帖子 ID。
	fetchErrors := 0
	cacheErrors := 0
	processedCount := 0

	for i := 0; i < len(postIDs); i += constant.BatchDetailSize {
		end := i + constant.BatchDetailSize
		if end > len(postIDs) {
			end = len(postIDs)
		}
		batchIDs := postIDs[i:end] // 当前批次的帖子 ID
		c.logger.Debug("开始处理批次帖子详情缓存", zap.Int("startIndex", i), zap.Int("batchSize", len(batchIDs)))

		// 3.1 使用带重试的逻辑从 MySQL 批量获取这批帖子的详情。
		//     fetchPostDetailsWithRetry 封装了重试逻辑。
		postDetails, fetchErr := c.fetchPostDetailsWithRetry(ctx, batchIDs)
		if fetchErr != nil {
			// 如果重试后仍然失败，记录错误并跳过当前批次。
			c.logger.Error("批量获取帖子详情最终失败，跳过此批次", zap.Error(fetchErr), zap.Int("startIndex", i), zap.Int("batchSize", len(batchIDs)))
			fetchErrors++
			continue // 处理下一批
		}
		if len(postDetails) != len(batchIDs) {
			// 可能部分 ID 在数据库中未找到详情，记录一下差异。
			c.logger.Warn("获取到的帖子详情数量与请求的 ID 数量不匹配", zap.Int("requested", len(batchIDs)), zap.Int("fetched", len(postDetails)), zap.Int("startIndex", i))
		}

		// 3.2 使用 Redis Pipeline 批量将获取到的详情写入缓存。
		pipe := c.redisClient.Pipeline()
		marshalErrorsInBatch := 0
		for _, detail := range postDetails {
			// 构造缓存 Key: "post_detail:{postID}"
			key := fmt.Sprintf(constant.PostDetailCacheKeyPrefix+"%d", detail.PostID)
			// 使用带重试的逻辑序列化详情实体为 JSON。
			jsonData, marshalErr := c.marshalDetailWithRetry(detail)
			if marshalErr != nil {
				// 序列化失败，记录错误，并将失败的 PostID 记录到 Redis list 以便后续排查。
				c.logger.Error("序列化帖子详情失败，跳过该详情", zap.Error(marshalErr), zap.Uint64("postID", detail.PostID))
				// 记录到失败列表 (RPUSH 是原子性的)
				logFailureErr := c.redisClient.RPush(ctx, "failed_post_details_serialize", fmt.Sprintf("%d", detail.PostID)).Err()
				if logFailureErr != nil {
					c.logger.Error("记录序列化失败的 PostID 到 Redis list 时出错", zap.Error(logFailureErr), zap.Uint64("postID", detail.PostID))
				}
				marshalErrorsInBatch++
				continue // 跳过这个详情
			}
			// 将 SET 命令（带 TTL）添加到 Pipeline。TTL 来自常量。
			pipe.Set(ctx, key, jsonData, 24*time.Hour) // 假设缓存 24 小时
		}

		// 3.3 执行 Pipeline。
		// 检查是否有成功序列化并准备好执行的命令
		commandsAddedCount := len(postDetails) - marshalErrorsInBatch
		if commandsAddedCount > 0 { // 只有当实际有命令被添加到 pipe 时才执行
			_, execErr := pipe.Exec(ctx)
			if execErr != nil {
				c.logger.Error("执行 Redis Pipeline 缓存帖子详情失败",
					zap.Error(execErr),
					zap.Int("startIndex", i),
					zap.Int("commandAttemptCount", commandsAddedCount)) // 记录尝试执行的命令数
				cacheErrors++
			} else {
				processedCount += commandsAddedCount // 累加成功处理的数量
				c.logger.Debug("成功执行批次帖子详情缓存 Pipeline",
					zap.Int("startIndex", i),
					zap.Int("successCount", commandsAddedCount))
			}
		} else if len(postDetails) > 0 { // 如果获取到了详情，但都序列化失败了
			c.logger.Warn("批次中有帖子详情但没有生成有效的缓存命令（可能全部序列化失败）",
				zap.Int("startIndex", i),
				zap.Int("detailsCount", len(postDetails)),
				zap.Int("marshalErrors", marshalErrorsInBatch))
		}
	} // 结束批次循环

	duration := time.Since(startTime)
	logFields := []zap.Field{
		zap.Duration("duration", duration),
		zap.Int("totalHotIDs", len(postIDs)),
		zap.Int("processedCount", processedCount),
		zap.Int("fetchErrors", fetchErrors),
		zap.Int("cacheErrors", cacheErrors),
	}
	if fetchErrors > 0 || cacheErrors > 0 {
		c.logger.Warn("完成同步热门帖子详情到 Redis (有错误)", logFields...)
		// 可以考虑返回一个表示部分失败的错误
		// return fmt.Errorf("同步帖子详情过程中发生错误: fetchBatches=%d, cacheBatches=%d", fetchErrors, cacheErrors)
	} else {
		c.logger.Info("成功完成同步热门帖子详情到 Redis", logFields...)
	}

	return nil // 即使有批次失败，整个任务也可能被视为完成（最终一致性）
}

// GetPostDetail 实现从 Redis 获取帖子详情。
func (c *cacheImpl) GetPostDetail(ctx context.Context, postID uint64) (*entities.PostDetail, error) {
	// 构造缓存 Key。
	key := fmt.Sprintf(constant.PostDetailCacheKeyPrefix+"%d", postID)
	c.logger.Debug("尝试从 Redis 获取帖子详情", zap.String("key", key))

	// 执行 GET 命令。
	jsonData, err := c.redisClient.Get(ctx, key).Result()
	if err != nil {
		// 区分缓存未命中 (redis.Nil) 和其他 Redis 错误。
		if errors.Is(err, redis.Nil) {
			c.logger.Info("帖子详情缓存未命中", zap.String("key", key))
			// 返回一个特定的未找到错误，方便上层判断是否需要回源。
			return nil, myErrors.ErrCacheMiss // 假设定义了缓存未命中错误
			// 或者 return nil, fmt.Errorf("缓存未命中")
		}
		// 其他 Redis 错误。
		c.logger.Error("从 Redis 获取帖子详情失败", zap.Error(err), zap.String("key", key))
		return nil, fmt.Errorf("获取帖子(ID: %d)详情缓存失败: %w", postID, err)
	}

	// 反序列化 JSON 数据。
	var postDetail entities.PostDetail
	if jsonErr := json.Unmarshal([]byte(jsonData), &postDetail); jsonErr != nil {
		c.logger.Error("反序列化帖子详情缓存数据失败", zap.Error(jsonErr), zap.String("key", key))
		// 缓存数据损坏，返回错误。可以考虑删除这个损坏的 key。
		// c.redisClient.Del(context.Background(), key)
		return nil, fmt.Errorf("解析帖子(ID: %d)详情缓存失败: %w", postID, jsonErr)
	}

	c.logger.Debug("成功从 Redis 获取帖子详情", zap.String("key", key))
	return &postDetail, nil
}

// cleanExpiredPostDetails 清理不再热门的帖子详情缓存。
// - 内部辅助函数。
func (c *cacheImpl) cleanExpiredPostDetails(ctx context.Context, currentHotPostIDs []uint64) error {
	c.logger.Info("开始清理过期的帖子详情缓存", zap.Int("currentHotCount", len(currentHotPostIDs)))
	startTime := time.Now()

	// 1. 将当前热榜 ID 列表转换为 map[string]bool 以便快速查找。
	//    Key 使用字符串形式的 ID。
	currentHotSet := make(map[string]bool, len(currentHotPostIDs))
	for _, id := range currentHotPostIDs {
		currentHotSet[fmt.Sprintf("%d", id)] = true
	}

	// 2. 使用 SCAN 迭代遍历 Redis 中所有匹配 "post_detail:*" 的 key。
	//    这是安全的方式，避免阻塞。
	var cursor uint64
	var keysToDelete []string
	scanCount := 0 // 记录扫描到的 key 数量

	for {
		// 每次扫描一批 key。
		scanKeys, nextCursor, err := c.redisClient.Scan(ctx, cursor, constant.PostDetailKeyPattern, 100).Result() // 每次扫描 100 个
		if err != nil {
			c.logger.Error("扫描 Redis post_detail keys 失败", zap.Error(err), zap.Uint64("cursor", cursor))
			// 如果 SCAN 失败，可能无法完成清理，但可以选择继续或返回错误。这里选择返回错误。
			return fmt.Errorf("扫描 Redis 键失败 (模式: %s): %w", constant.PostDetailKeyPattern, err)
		}
		scanCount += len(scanKeys)

		// 检查扫描到的 key 是否在当前热榜中。
		for _, key := range scanKeys {
			// 从 key (如 "post_detail:123") 中提取 ID 部分 ("123")。
			postIDStr := strings.TrimPrefix(key, constant.PostDetailCacheKeyPrefix)
			// 如果提取出的 ID 不在当前热榜 Set 中，则标记为待删除。
			if !currentHotSet[postIDStr] {
				keysToDelete = append(keysToDelete, key)
			}
		}

		// 更新游标。如果游标为 0，表示扫描完成。
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	} // 结束 SCAN 循环

	c.logger.Debug("SCAN 完成", zap.Int("scannedKeys", scanCount), zap.Int("toDeleteCount", len(keysToDelete)))

	// 3. 如果有需要删除的 key，使用 DEL 批量删除。
	if len(keysToDelete) > 0 {
		// DEL 命令可以接受多个 key。
		deletedCount, err := c.redisClient.Del(ctx, keysToDelete...).Result()
		if err != nil {
			// 删除失败记录错误，但不一定需要中断整个缓存同步任务。
			c.logger.Error("批量删除过期帖子详情缓存失败", zap.Error(err), zap.Int("deleteAttemptCount", len(keysToDelete)))
			// 不返回错误，允许主流程继续
		} else {
			c.logger.Info("成功删除过期帖子详情缓存", zap.Int64("deletedCount", deletedCount), zap.Int("attemptCount", len(keysToDelete)))
		}
	} else {
		c.logger.Info("没有需要清理的过期帖子详情缓存")
	}

	duration := time.Since(startTime)
	c.logger.Info("完成清理过期帖子详情缓存", zap.Duration("duration", duration))
	return nil
}

// fetchPostDetailsWithRetry 实现带重试的从 MySQL 获取帖子详情。
// - 内部辅助函数。
func (c *cacheImpl) fetchPostDetailsWithRetry(ctx context.Context, batchIDs []uint64) ([]*entities.PostDetail, error) {
	// 复制一份 ID 列表，避免修改原始切片。
	remainingIDs := make([]uint64, len(batchIDs))
	copy(remainingIDs, batchIDs)
	var allPostDetails []*entities.PostDetail // 存储所有成功获取的详情

	// 执行最多 MaxRetryTimes 次重试。
	for retry := 0; retry < constant.MaxRetryTimes; retry++ {
		c.logger.Debug("尝试批量获取帖子详情 (MySQL)", zap.Int("retry", retry+1), zap.Int("idCount", len(remainingIDs)))
		// 调用 PostDetailRepository 的方法批量获取。
		postDetails, err := c.postDetailRepo.GetPostDetailsByPostIDs(ctx, remainingIDs)
		if err != nil {
			// 如果数据库查询本身出错（例如连接问题）。
			c.logger.Error("批量获取帖子详情失败", zap.Error(err), zap.Int("retry", retry+1), zap.Int("idCount", len(remainingIDs)))
			// 如果是最后一次重试，则返回当前已获取的数据和错误。
			if retry == constant.MaxRetryTimes-1 {
				return allPostDetails, fmt.Errorf("获取帖子详情在 %d 次重试后仍然失败: %w", constant.MaxRetryTimes, err)
			}
			// 等待一段时间后重试。
			time.Sleep(time.Second * time.Duration(retry+1)) // 可以使用指数退避等策略
			continue                                         // 进入下一次重试
		}

		// 查询成功，将获取到的详情追加到总结果中。
		allPostDetails = append(allPostDetails, postDetails...)
		c.logger.Debug("批次获取帖子详情成功", zap.Int("fetchedCount", len(postDetails)), zap.Int("retry", retry+1))

		// 检查是否所有请求的 ID 都获取到了详情。
		// 如果获取到的数量等于请求的数量，说明全部成功，可以提前退出重试。
		if len(allPostDetails) >= len(batchIDs) { // 使用 >= 以防万一数据库返回了重复项？
			c.logger.Debug("已获取所有请求的帖子详情，提前退出重试")
			break
		}

		// 如果获取到的数量少于请求的数量，说明部分 ID 未找到对应的详情。
		// 找出哪些 ID 还没有获取到，准备下一次重试。
		successIDs := make(map[uint64]bool, len(postDetails))
		for _, detail := range postDetails {
			successIDs[detail.PostID] = true
		}

		// 更新 remainingIDs 为那些尚未成功的 ID。
		var nextRetryIDs []uint64
		for _, id := range remainingIDs { // 注意：这里应该遍历上次尝试的 remainingIDs
			if !successIDs[id] {
				nextRetryIDs = append(nextRetryIDs, id)
			}
		}
		remainingIDs = nextRetryIDs // 更新为下次需要重试的 ID 列表

		// 如果没有需要重试的 ID 了（理论上应该在上面 len(allPostDetails) >= len(batchIDs) 时退出）。
		if len(remainingIDs) == 0 {
			c.logger.Debug("所有帖子详情均已处理（部分可能未找到），退出重试")
			break
		}

		// 如果不是最后一次重试，等待后继续。
		if retry < constant.MaxRetryTimes-1 {
			c.logger.Warn("部分帖子详情未获取到，准备重试", zap.Int("remainingCount", len(remainingIDs)), zap.Uint64s("remainingIDs", remainingIDs))
			time.Sleep(time.Second * time.Duration(retry+1))
		}
	} // 结束重试循环

	// 检查最终获取的详情数量是否与请求的一致，如果不一致，记录警告。
	if len(allPostDetails) < len(batchIDs) {
		c.logger.Warn("最终获取到的帖子详情数量少于请求的 ID 数量", zap.Int("requested", len(batchIDs)), zap.Int("fetched", len(allPostDetails)))
		// 可以考虑找出具体哪些 ID 失败了并记录
	}

	return allPostDetails, nil // 返回所有成功获取的详情，不因部分未找到而返回错误
}

// marshalDetailWithRetry 实现带重试的 JSON 序列化。
// - 内部辅助函数。序列化通常不应失败，除非类型有问题。
func (c *cacheImpl) marshalDetailWithRetry(detail *entities.PostDetail) ([]byte, error) {
	var data []byte
	var err error
	for retry := 0; retry < constant.MaxRetryTimes; retry++ {
		data, err = json.Marshal(detail)
		// 如果序列化成功，立即返回。
		if err == nil {
			return data, nil
		}
		// 序列化失败，记录错误并等待短暂时间后重试。
		c.logger.Error("序列化帖子详情失败", zap.Error(err), zap.Int("retry", retry+1), zap.Uint64("postID", detail.PostID))
		time.Sleep(time.Millisecond * 100 * time.Duration(retry+1)) // 短暂等待
		// 如果是最后一次重试，返回最终的错误。
		if retry == constant.MaxRetryTimes-1 {
			return nil, fmt.Errorf("序列化帖子详情(ID: %d)在 %d 次重试后失败: %w", detail.PostID, constant.MaxRetryTimes, err)
		}
	}
	// 理论上不会执行到这里，因为循环要么成功返回，要么在最后一次重试失败时返回。
	// 添加一个返回值以满足编译器。
	return nil, err
}

// 需要导入 "time", "strings", "strconv", "encoding/json", "errors" 包
// import "time"
// import "strings"
// import "strconv"
// import "encoding/json"
// import "errors"
// 假设 commonerrors 已导入或在此定义 ErrCacheMiss
// import "github.com/Xushengqwer/go-common/commonerrors"
