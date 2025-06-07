package redis

import (
	"context"
	"fmt"
	"github.com/Xushengqwer/post_service/config"
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

	// GetAllViewCounts 使用 SCAN 命令分批获取 Redis 中所有帖子的浏览量计数。
	// - 目的是安全、高效地获取全量浏览量数据，作为同步到 MySQL 的数据源。
	// - 使用 SCAN 避免一次性 KEYS 操作阻塞 Redis，MGET 批量获取提高效率。
	// - 输入: ctx (上下文)。
	// - 输出: map[uint64]int64 (帖子 ID -> 浏览量), error 操作错误。
	GetAllViewCounts(ctx context.Context) (map[uint64]int64, error)
}

// postViewRepository 是 PostViewRepository 接口的 Redis 实现。
type postViewRepository struct {
	redisClient       *redis.Client         // Redis 客户端实例
	logger            *core.ZapLogger       // 日志记录器实例
	viewSyncCfg       config.ViewSyncConfig // 新增：用于存储浏览量同步相关的配置，包括 ScanBatchSize
	bloomFilterSize   int64                 // Bloom Filter 配置: 预期容量
	bloomFilterHashes uint                  // Bloom Filter 配置: 哈希函数数量 (影响精度和空间)
	bloomErrorRate    float64               // Bloom Filter 配置: 可接受的误判率
}

// NewPostViewRepository 创建 PostViewRepository 实例。
// - 通过依赖注入传入 redisClient 和 logger。
// - Bloom Filter 相关参数也在此设置。
func NewPostViewRepository(redisClient *redis.Client, logger *core.ZapLogger, bloomFilterSize int64, bloomFilterHashes uint, bloomErrorRate float64, viewSyncCfg config.ViewSyncConfig) PostViewRepository { // 添加 logger 参数
	return &postViewRepository{
		redisClient:       redisClient,
		logger:            logger,      // 初始化 logger
		viewSyncCfg:       viewSyncCfg, // 存储配置
		bloomFilterSize:   bloomFilterSize,
		bloomFilterHashes: bloomFilterHashes,
		bloomErrorRate:    bloomErrorRate,
	}
}

// IncrementViewCount 实现增加帖子浏览量的逻辑。
// 核心功能：使用 Bloom Filter 防止用户短时间内重复刷量，并原子性地增加帖子浏览数及更新其在排行榜中的分数。
func (r *postViewRepository) IncrementViewCount(ctx context.Context, postID uint64, userID string) error {
	// 1. 构造 Redis Key
	bloomKey := fmt.Sprintf("%s%d", constant.PostViewBloomPrefix, postID)
	viewCountKey := fmt.Sprintf("%s%d", constant.PostViewCountPrefix, postID)
	postsRankKey := constant.PostsRankKey

	// 2. 确保 Bloom Filter 已按需创建
	// 直接调用 BF.RESERVE。
	// 如果过滤器已存在，BF.RESERVE 可能会返回 "ERR item exists"，我们将其视为正常情况。
	if err := r.redisClient.BFReserve(ctx, bloomKey, r.bloomErrorRate, r.bloomFilterSize).Err(); err != nil {
		// 检查错误消息是否明确指示 "item exists"。
		if err != nil && strings.Contains(err.Error(), "ERR item exists") {
			r.logger.Debug("尝试创建 Bloom Filter 时发现其已存在 (此为正常情况)",
				zap.String("bloomKey", bloomKey),
				zap.String("originalError", err.Error()),
			)
		} else {
			// 对于其他类型的 BF.RESERVE 错误，则认为是真正的失败。
			r.logger.Error("创建或调整 Bloom Filter 失败", zap.Error(err), zap.String("bloomKey", bloomKey))
			return fmt.Errorf("创建或调整 Bloom Filter '%s' 失败: %w", bloomKey, err)
		}
	} else {
		r.logger.Info("Bloom Filter 已确保存在/已创建", zap.String("bloomKey", bloomKey))
	}

	// 3. 使用 Bloom Filter 判断用户是否已浏览 (防刷核心)
	userExists, err := r.redisClient.BFExists(ctx, bloomKey, userID).Result()
	if err != nil {
		r.logger.Error("检查用户是否在 Bloom Filter 中时出错", zap.Error(err), zap.String("bloomKey", bloomKey), zap.String("userID", userID))
		return fmt.Errorf("检查 Bloom Filter 出错 ('%s', '%s'): %w", bloomKey, userID, err)
	}
	if userExists {
		r.logger.Debug("用户已在 Bloom Filter 中，跳过计数", zap.String("bloomKey", bloomKey), zap.String("userID", userID), zap.Uint64("postID", postID))
		return nil
	}

	// 4. 将用户添加到 Bloom Filter 并设置/刷新过期时间
	_, err = r.redisClient.BFAdd(ctx, bloomKey, userID).Result()
	if err != nil {
		r.logger.Error("添加用户到 Bloom Filter 失败", zap.Error(err), zap.String("bloomKey", bloomKey), zap.String("userID", userID))
		return fmt.Errorf("添加用户到 Bloom Filter '%s' 失败: %w", bloomKey, err)
	}

	// 确保 Bloom Filter 有过期时间，定义防刷窗口，并刷新它。
	// 直接使用 constant.BloomViewTTL。
	// 确保 constant.BloomViewTTL 在您的 constant 包中已定义为 time.Duration 类型。
	// 例如: const BloomViewTTL time.Duration = 12 * time.Hour
	if err := r.redisClient.Expire(ctx, bloomKey, constant.BloomViewTTL).Err(); err != nil { // 直接使用常量
		r.logger.Warn("设置 Bloom Filter 过期时间失败，但不中断计数", zap.Error(err), zap.String("bloomKey", bloomKey))
	}

	// 5. 原子性增加浏览量并更新排行榜 (Lua 脚本)
	luaScript := redis.NewScript(`
        local viewCount = redis.call("INCR", KEYS[1])
        redis.call("ZADD", KEYS[2], viewCount, ARGV[1])
        return viewCount
    `)

	_, err = luaScript.Run(ctx, r.redisClient, []string{viewCountKey, postsRankKey}, postID).Result()
	if err != nil {
		r.logger.Error("Lua 脚本执行失败：增加浏览量和更新排名", zap.Error(err), zap.Uint64("postID", postID))
		return fmt.Errorf("原子性增加浏览量失败 (PostID: %d): %w", postID, err)
	}

	r.logger.Debug("成功增加浏览量并更新排名", zap.Uint64("postID", postID))
	return nil
}

// GetAllViewCounts 使用 SCAN 命令安全地迭代并获取所有帖子的浏览量。
// 此方法主要用于定时任务，将 Redis 中的全量浏览数据同步到持久化存储（如 MySQL）。
func (r *postViewRepository) GetAllViewCounts(ctx context.Context) (map[uint64]int64, error) {
	viewCounts := make(map[uint64]int64)
	var cursor uint64 = 0 // SCAN 命令的初始游标
	// 直接使用 PostViewCountPrefix 构建 SCAN 的匹配模式。
	matchPattern := constant.PostViewCountPrefix + "*"
	// 从配置中读取 scanCount，并提供 fallback。
	scanCount := r.viewSyncCfg.ScanBatchSize
	if scanCount <= 0 {
		scanCount = 1000 // Fallback if config value is invalid or zero
		r.logger.Warn("GetAllViewCounts: 配置中的 ScanBatchSize 无效或为零，使用默认值。",
			zap.Int64("defaultScanBatchSize", scanCount),
			zap.Int64("configuredScanBatchSize", r.viewSyncCfg.ScanBatchSize),
		)
	}

	r.logger.Info("开始扫描 Redis 获取所有帖子浏览量",
		zap.String("pattern", matchPattern),
		zap.Int64("scan_batch_size", scanCount),
	)
	startTime := time.Now()

	// 使用 for 循环和 SCAN 命令迭代，直到游标返回 0，表示遍历完成。
	for {
		keys, nextCursor, err := r.redisClient.Scan(ctx, cursor, matchPattern, scanCount).Result()
		if err != nil {
			r.logger.Error("执行 Redis SCAN 命令失败",
				zap.Error(err),
				zap.Uint64("cursor", cursor),
				zap.String("pattern", matchPattern),
			)
			return nil, fmt.Errorf("扫描 Redis Keys 失败 (模式: %s): %w", matchPattern, err)
		}

		if len(keys) > 0 {
			r.logger.Debug("SCAN 批次获取到 Keys", zap.Int("count", len(keys)), zap.Uint64("current_scan_cursor", cursor))

			values, mgetErr := r.redisClient.MGet(ctx, keys...).Result()
			if mgetErr != nil {
				r.logger.Error("执行 Redis MGET 命令批量获取浏览量失败",
					zap.Error(mgetErr),
					zap.Strings("keys_in_batch", keys),
				)
				return nil, fmt.Errorf("批量获取浏览量值失败 (%d keys): %w", len(keys), mgetErr)
			}

			for i, key := range keys {
				postIDStr := strings.TrimPrefix(key, constant.PostViewCountPrefix)
				postID, parseErr := strconv.ParseUint(postIDStr, 10, 64)
				if parseErr != nil {
					r.logger.Error("从 Redis Key 解析 PostID 失败，已跳过该 Key。",
						zap.Error(parseErr),
						zap.String("key", key),
					)
					continue
				}

				viewCount := int64(0)
				if i < len(values) && values[i] != nil {
					if valueStr, ok := values[i].(string); ok && valueStr != "" {
						parsedCount, parseCountErr := strconv.ParseInt(valueStr, 10, 64)
						if parseCountErr != nil {
							r.logger.Error("解析 Redis 中的浏览量值失败，该帖子浏览量将视为 0。",
								zap.Error(parseCountErr),
								zap.String("key", key),
								zap.String("value_str", valueStr),
							)
						} else {
							viewCount = parsedCount
						}
					} else {
						r.logger.Warn("Redis Key 的值类型不是有效字符串或为空，该帖子浏览量将视为 0。",
							zap.String("key", key),
							zap.Any("value_type", fmt.Sprintf("%T", values[i])),
							zap.Any("value", values[i]),
						)
					}
				} else {
					r.logger.Warn("MGET 未能获取到 Key 的值 (可能已删除或类型错误)，该帖子浏览量将视为 0。",
						zap.String("key", key),
						zap.Int("value_index", i),
						zap.Int("values_len", len(values)),
					)
				}
				viewCounts[postID] = viewCount
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	duration := time.Since(startTime)
	r.logger.Info("完成扫描 Redis 帖子浏览量",
		zap.Int("total_unique_posts_found", len(viewCounts)),
		zap.Duration("duration", duration),
	)
	return viewCounts, nil
}
