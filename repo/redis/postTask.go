// File: repo/redis/postTask.go
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/models/entities"
	"github.com/Xushengqwer/post_service/models/vo" // 确保 vo 包已导入
	"github.com/Xushengqwer/post_service/repo/mysql"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// PostTaskCache 定义了后台任务管理和维护帖子相关缓存的操作接口。
type PostTaskCache interface {
	// CreateHotList 原子性地从总排行榜 (`PostsRankKey`) 截取前 N 条记录，生成/覆盖热榜 (`HotPostsRankKey`)。
	// 此方法负责生成后续缓存方法所依赖的热榜快照。
	CreateHotList(ctx context.Context, n int) error

	// CacheHotPostsToRedis 将MySQL中的帖子基础信息加载到redis中
	CacheHotPostsToRedis(ctx context.Context) error

	// CacheHotPostDetailsToRedis  将MySQL中的帖子详情信息加载到redis中
	CacheHotPostDetailsToRedis(ctx context.Context) error
}

// postTaskCacheImpl 是 PostTaskCache 接口的 Redis 实现。
type postTaskCacheImpl struct {
	redisClient *redis.Client
	logger      *core.ZapLogger
	postBatch   mysql.PostBatchOperationsRepository
}

// NewPostTaskCacheImpl 创建 PostTaskCache 的新实例。
func NewPostTaskCacheImpl(
	redisClient *redis.Client,
	logger *core.ZapLogger,
	postBatch mysql.PostBatchOperationsRepository,
) PostTaskCache {
	return &postTaskCacheImpl{
		redisClient: redisClient,
		logger:      logger,
		postBatch:   postBatch,
	}
}

// CreateHotList 原子性地从总排行榜截取前 N 条记录，生成或覆盖热榜。
func (c *postTaskCacheImpl) CreateHotList(ctx context.Context, n int) error {
	if n <= 0 {
		c.logger.Info("CreateHotList: 请求创建的热榜大小 n 小于或等于 0，操作取消。", zap.Int("n", n))
		return nil
	}

	fullRankKey := constant.PostsRankKey
	hotListKey := constant.HotPostsRankKey

	c.logger.Info("开始创建/更新热榜快照",
		zap.String("sourceKey", fullRankKey),
		zap.String("destinationKey", hotListKey),
		zap.Int("size_n", n),
	)

	// 修正后的 Lua 脚本：
	// ZREVRANGE WITHSCORES 返回 {member1, score1, member2, score2, ...}
	// ZADD 需要 {score1, member1, score2, member2, ...}
	// 因此，我们需要在 Lua 中重新构造参数列表或迭代添加。
	luaScript := redis.NewScript(`
		-- KEYS[1]: source ZSet (total rank: constant.PostsRankKey)
		-- KEYS[2]: destination ZSet (hot list: constant.HotPostsRankKey)
		-- ARGV[1]: number of items to copy (n)

		local items_with_scores = redis.call("ZREVRANGE", KEYS[1], 0, tonumber(ARGV[1]) - 1, "WITHSCORES")
		redis.call("DEL", KEYS[2])

		if #items_with_scores > 0 then
			local args_for_zadd = { KEYS[2] } -- Start with the key for ZADD
			for i = 1, #items_with_scores, 2 do
				-- items_with_scores[i] is member, items_with_scores[i+1] is score
				table.insert(args_for_zadd, items_with_scores[i+1]) -- Add score
				table.insert(args_for_zadd, items_with_scores[i])   -- Add member
			end
			redis.call("ZADD", unpack(args_for_zadd))
		end
		return #items_with_scores / 2 -- Returns the number of members processed
	`)

	_, err := luaScript.Run(ctx, c.redisClient, []string{fullRankKey, hotListKey}, n).Result()
	if err != nil {
		c.logger.Error("执行 Lua 脚本创建热榜快照失败",
			zap.Error(err),
			zap.String("sourceKey", fullRankKey),
			zap.String("destinationKey", hotListKey),
			zap.Int("n", n),
		)
		return fmt.Errorf("创建热榜快照 (Top %d) 失败: %w", n, err)
	}

	c.logger.Info("成功创建/更新热榜快照",
		zap.String("key", hotListKey),
		zap.Int("requested_size_n", n),
	)
	return nil
}

// CacheHotPostsToRedis 将热门帖子缓存到 Redis Hash。
func (c *postTaskCacheImpl) CacheHotPostsToRedis(ctx context.Context) error {
	startTime := time.Now()
	c.logger.Info("开始同步热门帖子到 Redis Hash (采用临时Key+RENAME策略)")

	hotListKey := constant.HotPostsRankKey
	finalHashKey := constant.PostsHashKey
	tempHashKey := finalHashKey + "_temp_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	postScores, err := c.redisClient.ZRevRangeWithScores(ctx, hotListKey, 0, int64(constant.HotPostsCacheSize-1)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			c.logger.Info("热榜 ZSet (快照) 为空，将清空帖子 Hash 缓存", zap.String("hashKeyToClear", finalHashKey))
			if delErr := c.redisClient.Del(ctx, finalHashKey).Err(); delErr != nil {
				c.logger.Error("清空帖子 Hash 缓存失败", zap.Error(delErr), zap.String("key", finalHashKey))
			}
			return nil
		}
		c.logger.Error("从热榜 ZSet (快照) 获取帖子分数失败", zap.Error(err), zap.String("key", hotListKey))
		return fmt.Errorf("获取热榜 ZSet (快照) 失败: %w", err)
	}

	currentHotPostIDs := make([]uint64, 0, len(postScores))
	currentScoreMap := make(map[string]float64) // Key: postID string, Value: score (viewCount from snapshot)
	for _, z := range postScores {
		idStr, ok := z.Member.(string)
		if !ok {
			errMsg := fmt.Sprintf("热榜 ZSet (key: %s) 成员类型非字符串 (member: %v)，数据异常", hotListKey, z.Member)
			c.logger.Error(errMsg)
			return errors.New(errMsg)
		}
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			errMsg := fmt.Sprintf("解析热榜 ZSet (key: %s) 成员 ID '%s' 失败: %v，数据异常", hotListKey, idStr, parseErr)
			c.logger.Error(errMsg)
			return errors.New(errMsg)
		}
		currentHotPostIDs = append(currentHotPostIDs, id)
		currentScoreMap[idStr] = z.Score
	}

	if len(currentHotPostIDs) == 0 {
		c.logger.Info("热榜 ZSet (快照) 中没有有效帖子 ID，将清空帖子 Hash 缓存", zap.String("hashKeyToClear", finalHashKey))
		if delErr := c.redisClient.Del(ctx, finalHashKey).Err(); delErr != nil {
			c.logger.Error("清空帖子 Hash 缓存失败", zap.Error(delErr), zap.String("key", finalHashKey))
		}
		return nil
	}
	c.logger.Debug("从热榜 ZSet (快照) 解析完成", zap.Int("hotPostCount", len(currentHotPostIDs)))

	postsFromDB, dbErr := c.postBatch.GetPostsByIDs(ctx, currentHotPostIDs)
	if dbErr != nil {
		c.logger.Error("从 MySQL 批量获取热门帖子失败，本次缓存更新中止，现有缓存将保留。",
			zap.Error(dbErr), zap.Int("idCount", len(currentHotPostIDs)))
		return fmt.Errorf("从数据库获取帖子数据失败: %w", dbErr)
	}
	c.logger.Debug("从 MySQL 获取热门帖子数据成功", zap.Int("fetchedCount", len(postsFromDB)))

	dataToCache := make(map[string]interface{})
	marshalErrors := 0
	dbPostsMap := make(map[uint64]*entities.Post)
	for _, p := range postsFromDB {
		dbPostsMap[p.ID] = p
	}

	for _, hotID := range currentHotPostIDs {
		idStr := fmt.Sprintf("%d", hotID)
		post, foundInDB := dbPostsMap[hotID]
		if !foundInDB {
			c.logger.Warn("热榜中的 PostID 在数据库中未找到，无法缓存该帖子", zap.Uint64("postID", hotID))
			continue
		}
		postToCache := *post
		if score, scoreExists := currentScoreMap[idStr]; scoreExists {
			postToCache.ViewCount = int64(score) // 使用 ZSet 快照中的分数作为浏览量
		} else {
			c.logger.Error("严重数据不一致：热榜 ZSet (快照) 分数中未找到 PostID，将使用DB中的ViewCount",
				zap.Uint64("postID", hotID), zap.String("zsetKey", hotListKey))
			// 保持 postToCache.ViewCount 为从 DB 读取的值
		}
		jsonData, jsonErr := json.Marshal(postToCache)
		if jsonErr != nil {
			c.logger.Error("序列化帖子实体失败，跳过该帖子", zap.Error(jsonErr), zap.Uint64("postID", postToCache.ID))
			marshalErrors++
			continue
		}
		dataToCache[idStr] = jsonData
	}

	if len(dataToCache) == 0 {
		c.logger.Error("未能准备任何有效的帖子数据进行缓存 (DB未找到或序列化失败)，现有缓存将保留。",
			zap.Int("hotIDsFromZset", len(currentHotPostIDs)),
			zap.Int("dbPostsFetched", len(postsFromDB)),
			zap.Int("marshalErrors", marshalErrors),
		)
		return errors.New("未能准备有效的帖子数据进行缓存，操作中止")
	}

	pipe := c.redisClient.Pipeline()
	pipe.Del(ctx, tempHashKey)
	if hmSetCmdErr := pipe.HMSet(ctx, tempHashKey, dataToCache).Err(); hmSetCmdErr != nil {
		c.logger.Error("构造 HMSet 命令到 Pipeline 失败", zap.Error(hmSetCmdErr), zap.String("tempHashKey", tempHashKey))
		c.redisClient.Del(ctx, tempHashKey)
		return fmt.Errorf("构造 HMSet 命令 (key: %s) 失败: %w", tempHashKey, hmSetCmdErr)
	}
	_, execErr := pipe.Exec(ctx)
	if execErr != nil {
		c.logger.Error("执行 Redis Pipeline (写入临时 Hash) 失败，现有缓存将保留。",
			zap.Error(execErr), zap.String("tempHashKey", tempHashKey))
		c.redisClient.Del(ctx, tempHashKey)
		return fmt.Errorf("写入临时帖子 Hash 缓存 (key: %s) 失败: %w", tempHashKey, execErr)
	}

	if renameErr := c.redisClient.Rename(ctx, tempHashKey, finalHashKey).Err(); renameErr != nil {
		c.logger.Error("执行 Redis RENAME (temp 到 final Hash) 失败，新缓存可能在临时Key中，现有缓存可能仍存在。",
			zap.Error(renameErr),
			zap.String("tempHashKey", tempHashKey),
			zap.String("finalHashKey", finalHashKey),
		)
		c.redisClient.Del(ctx, tempHashKey)
		return fmt.Errorf("重命名临时 Hash (key: %s) 到最终 Hash (key: %s) 失败: %w", tempHashKey, finalHashKey, renameErr)
	}

	c.logger.Info("成功将热门帖子同步到 Redis Hash (采用临时Key+RENAME策略)",
		zap.String("finalHashKey", finalHashKey),
		zap.Int("cachedCount", len(dataToCache)),
		zap.Int("marshalErrors", marshalErrors),
	)

	duration := time.Since(startTime)
	c.logger.Info("完成同步热门帖子到 Redis Hash 任务", zap.Duration("duration", duration))
	return nil
}

// CacheHotPostDetailsToRedis 实现缓存热门帖子详情的逻辑。
// 此方法依赖于外部调用者已通过 CreateHotList (现在是 PostTaskCache 的一部分) 更新了 constant.HotPostsRankKey (热榜快照)。
func (c *postTaskCacheImpl) CacheHotPostDetailsToRedis(ctx context.Context) error {
	startTime := time.Now()
	c.logger.Info("开始同步热门帖子详情到 Redis (基于已生成的热榜快照, 采用临时Key+RENAME及差量更新策略)")

	// 1. 从热榜 ZSet (`constant.HotPostsRankKey`) 获取当前热门帖子ID和分数(浏览量)
	hotListKey := constant.HotPostsRankKey
	postScores, err := c.redisClient.ZRevRangeWithScores(ctx, hotListKey, 0, int64(constant.HotPostsCacheSize-1)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			c.logger.Info("热榜 ZSet (快照) 为空，无需同步详情缓存。将清理所有旧详情缓存。")
			var allDetailKeys []string
			var cursor uint64
			scanPattern := constant.PostDetailCacheKeyPrefix + "*"
			scanCount := int64(1000)
			for {
				keys, nextCursor, scanErr := c.redisClient.Scan(ctx, cursor, scanPattern, scanCount).Result()
				if scanErr != nil {
					c.logger.Error("扫描所有帖子详情Key失败，无法在热榜为空时完全清理", zap.Error(scanErr))
					break
				}
				allDetailKeys = append(allDetailKeys, keys...)
				cursor = nextCursor
				if cursor == 0 {
					break
				}
			}
			if len(allDetailKeys) > 0 {
				if delErr := c.redisClient.Del(ctx, allDetailKeys...).Err(); delErr != nil {
					c.logger.Error("在热榜为空时清理所有帖子详情缓存失败", zap.Error(delErr), zap.Int("keysToDelete", len(allDetailKeys)))
				} else {
					c.logger.Info("热榜为空，已清理所有旧的帖子详情缓存", zap.Int("deletedCount", len(allDetailKeys)))
				}
			}
			return nil
		}
		c.logger.Error("从热榜 ZSet (快照) 获取热门帖子列表（带分数）失败", zap.Error(err), zap.String("key", hotListKey))
		return fmt.Errorf("从热榜 ZSet (快照) 获取热门帖子列表（带分数）失败: %w", err)
	}

	currentHotPostIDs := make([]uint64, 0, len(postScores))
	currentHotPostScoresMap := make(map[uint64]float64, len(postScores)) // postID (uint64) -> score
	for _, z := range postScores {
		idStr, ok := z.Member.(string)
		if !ok {
			errMsg := fmt.Sprintf("热榜 ZSet (快照 key: %s) 成员类型非字符串 (member: %v)，数据异常", hotListKey, z.Member)
			c.logger.Error(errMsg)
			return errors.New(errMsg)
		}
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr != nil {
			errMsg := fmt.Sprintf("解析热榜 ZSet (快照 key: %s) 成员 ID '%s' 失败: %v，数据异常", hotListKey, idStr, parseErr)
			c.logger.Error(errMsg)
			return errors.New(errMsg)
		}
		currentHotPostIDs = append(currentHotPostIDs, id)
		currentHotPostScoresMap[id] = z.Score
	}

	if len(currentHotPostIDs) == 0 {
		c.logger.Info("热榜 ZSet (快照) 中没有有效帖子 ID，将清理所有帖子详情缓存。")
		var allDetailKeys []string
		var cursor uint64
		scanPattern := constant.PostDetailCacheKeyPrefix + "*"
		scanCount := int64(1000)
		for {
			keys, nextCursor, scanErr := c.redisClient.Scan(ctx, cursor, scanPattern, scanCount).Result()
			if scanErr != nil {
				c.logger.Error("扫描所有帖子详情Key失败，无法在热榜为空时完全清理", zap.Error(scanErr))
				break
			}
			allDetailKeys = append(allDetailKeys, keys...)
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
		if len(allDetailKeys) > 0 {
			if delErr := c.redisClient.Del(ctx, allDetailKeys...).Err(); delErr != nil {
				c.logger.Error("在热榜为空时清理所有帖子详情缓存失败", zap.Error(delErr), zap.Int("keysToDelete", len(allDetailKeys)))
			} else {
				c.logger.Info("热榜为空，已清理所有旧的帖子详情缓存", zap.Int("deletedCount", len(allDetailKeys)))
			}
		}
		return nil
	}
	c.logger.Debug("从热榜 ZSet (快照) 获取到当前热门帖子ID和分数", zap.Int("count", len(currentHotPostIDs)))
	currentHotPostIDsSet := make(map[uint64]bool, len(currentHotPostIDs))
	for _, id := range currentHotPostIDs {
		currentHotPostIDsSet[id] = true
	}

	// 2. 获取当前已缓存的帖子详情ID (SCAN逻辑内联)
	var cachedDetailKeys []string
	var cursor uint64
	scanPattern := constant.PostDetailCacheKeyPrefix + "*"
	scanCount := int64(1000)
	c.logger.Debug("开始扫描已缓存的帖子详情Key", zap.String("pattern", scanPattern), zap.Int64("scanCount", scanCount))
	for {
		keys, nextCursor, scanErr := c.redisClient.Scan(ctx, cursor, scanPattern, scanCount).Result()
		if scanErr != nil {
			c.logger.Error("扫描已缓存的帖子详情Key失败，无法进行差量更新，中止任务。", zap.Error(scanErr), zap.String("pattern", scanPattern), zap.Uint64("cursor", cursor))
			return fmt.Errorf("扫描已缓存详情Key (pattern: %s) 失败: %w", scanPattern, scanErr)
		}
		cachedDetailKeys = append(cachedDetailKeys, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	c.logger.Debug("扫描到已缓存的帖子详情Key", zap.Int("count", len(cachedDetailKeys)))

	cachedDetailIDsMap := make(map[uint64]string, len(cachedDetailKeys)) // postID -> fullFinalKey
	for _, key := range cachedDetailKeys {
		if !strings.HasPrefix(key, constant.PostDetailCacheKeyPrefix) || strings.Contains(key[len(constant.PostDetailCacheKeyPrefix):], ":temp:") {
			continue
		}
		idStr := strings.TrimPrefix(key, constant.PostDetailCacheKeyPrefix)
		id, parseErr := strconv.ParseUint(idStr, 10, 64)
		if parseErr == nil {
			cachedDetailIDsMap[id] = key
		} else {
			c.logger.Warn("解析已缓存的帖子详情Key中的ID失败，跳过", zap.String("key", key), zap.Error(parseErr))
		}
	}

	// 3. 计算差异
	var idsToFetchAndAggregate []uint64
	var finalKeysToDelete []string

	for hotID := range currentHotPostIDsSet {
		idsToFetchAndAggregate = append(idsToFetchAndAggregate, hotID)
	}
	for cachedID, finalKey := range cachedDetailIDsMap {
		if _, isStillHot := currentHotPostIDsSet[cachedID]; !isStillHot {
			finalKeysToDelete = append(finalKeysToDelete, finalKey)
		}
	}
	c.logger.Debug("计算缓存差异完成",
		zap.Int("toFetchAndAggregateOrRefresh", len(idsToFetchAndAggregate)),
		zap.Int("toDelete", len(finalKeysToDelete)),
	)

	// 4. 阶段一：获取、聚合新详情并写入临时缓存区
	var marshalErrorCountInStage1 int = 0
	tempKeyToFinalKeyMap := make(map[string]string)

	if len(idsToFetchAndAggregate) > 0 {
		c.logger.Info("需要获取、聚合并缓存/刷新帖子详情", zap.Int("count", len(idsToFetchAndAggregate)))

		postsData, dbErrPosts := c.postBatch.GetPostsByIDs(ctx, idsToFetchAndAggregate)
		if dbErrPosts != nil {
			c.logger.Error("从MySQL批量获取帖子基本信息失败，操作中止，不修改现有缓存。", zap.Error(dbErrPosts))
			return fmt.Errorf("数据库获取帖子基本信息失败: %w", dbErrPosts)
		}
		postsMap := make(map[uint64]*entities.Post, len(postsData))
		for _, p := range postsData {
			postsMap[p.ID] = p
		}
		c.logger.Debug("从MySQL获取帖子基本信息", zap.Int("count", len(postsData)))

		detailsData, dbErrDetails := c.postBatch.GetPostDetailsByPostIDs(ctx, idsToFetchAndAggregate)
		if dbErrDetails != nil {
			c.logger.Error("从MySQL批量获取帖子详细内容失败，操作中止，不修改现有缓存。", zap.Error(dbErrDetails))
			return fmt.Errorf("数据库获取帖子详细内容失败: %w", dbErrDetails)
		}
		detailsMap := make(map[uint64]*entities.PostDetail, len(detailsData))
		postDetailIDsForImageQuery := make([]uint64, 0, len(detailsData)) // 用于查询图片的 post_details.id 列表
		for _, d := range detailsData {
			detailsMap[d.PostID] = d
			postDetailIDsForImageQuery = append(postDetailIDsForImageQuery, d.ID) // 收集 post_details 表的 ID
		}
		c.logger.Debug("从MySQL获取帖子详细内容", zap.Int("count", len(detailsData)), zap.Int("postDetailIDsForImageQueryCount", len(postDetailIDsForImageQuery)))

		// 4.3 批量获取帖子详情图片 (post_detail_images 表)
		detailImagesMap := make(map[uint64][]*entities.PostDetailImage) // key 是 post_details.id
		if len(postDetailIDsForImageQuery) > 0 {
			var dbErrImages error
			detailImagesMap, dbErrImages = c.postBatch.BatchGetPostDetailImages(ctx, postDetailIDsForImageQuery)
			if dbErrImages != nil {
				c.logger.Error("从MySQL批量获取帖子详情图片失败，将不带图片信息继续聚合，但不中止操作。", zap.Error(dbErrImages))
				// 不中止，但记录错误，后续聚合时图片字段会为空
			} else {
				c.logger.Debug("从MySQL获取帖子详情图片", zap.Int("detailImageSetsCount", len(detailImagesMap)))
			}
		}

		if len(postsData) > 0 || len(detailsData) > 0 {
			pipe := c.redisClient.Pipeline()
			tempKeyWritesAttempted := 0

			for _, postIDToProcess := range idsToFetchAndAggregate {
				post, postFound := postsMap[postIDToProcess]
				detail, detailFound := detailsMap[postIDToProcess]

				if !postFound || !detailFound {
					c.logger.Warn("无法聚合帖子详情：Post或PostDetail信息不完整", zap.Uint64("postID", postIDToProcess), zap.Bool("postFound", postFound), zap.Bool("detailFound", detailFound))
					continue
				}

				viewCountFromSnapshot := post.ViewCount // 默认使用DB中的值
				if score, ok := currentHotPostScoresMap[postIDToProcess]; ok {
					viewCountFromSnapshot = int64(score)
				} else {
					c.logger.Warn("在热榜快照分数中未找到PostID，将使用DB中的ViewCount进行详情缓存", zap.Uint64("postID", postIDToProcess))
				}

				// 准备图片VO列表
				var imageVOs []vo.PostImageVO
				if images, imagesFound := detailImagesMap[detail.ID]; imagesFound { // 使用 detail.ID (post_details 表的主键) 作为 key
					for _, imgEntity := range images {
						imageVOs = append(imageVOs, vo.PostImageVO{
							ImageURL:     imgEntity.ImageURL,
							DisplayOrder: imgEntity.DisplayOrder,
							ObjectKey:    imgEntity.ObjectKey,
						})
					}
				}

				postDetailVO := vo.PostDetailVO{
					//  POST实体的部分
					ID:             post.ID,
					Title:          post.Title,
					AuthorID:       post.AuthorID,
					AuthorAvatar:   post.AuthorAvatar,
					AuthorUsername: post.AuthorUsername,
					ViewCount:      viewCountFromSnapshot, // 使用来自热榜快照的浏览量
					OfficialTag:    post.OfficialTag,
					CreatedAt:      post.CreatedAt,
					UpdatedAt:      post.UpdatedAt,

					// post_detail实体的部分
					Content:      detail.Content,
					PricePerUnit: detail.PricePerUnit,
					ContactInfo:  detail.ContactInfo,

					// 详情图的部分
					Images: imageVOs,
				}

				idStr := strconv.FormatUint(postDetailVO.ID, 10)
				jsonData, jsonErr := json.Marshal(postDetailVO)
				if jsonErr != nil {
					c.logger.Error("序列化聚合后的帖子详情VO失败，跳过", zap.Error(jsonErr), zap.Uint64("postID", postDetailVO.ID))
					marshalErrorCountInStage1++
					continue
				}
				tempKey := constant.PostDetailCacheKeyPrefix + "temp:" + idStr
				finalKey := constant.PostDetailCacheKeyPrefix + idStr

				pipe.Set(ctx, tempKey, jsonData, 0)
				tempKeyToFinalKeyMap[tempKey] = finalKey
				tempKeyWritesAttempted++
			}

			if tempKeyWritesAttempted > 0 {
				_, execErr := pipe.Exec(ctx)
				if execErr != nil {
					c.logger.Error("Pipeline执行失败：写入聚合帖子详情到临时Key时出错，操作中止，不修改现有缓存。",
						zap.Error(execErr), zap.Int("attemptedTempKeyWrites", tempKeyWritesAttempted))
					if len(tempKeyToFinalKeyMap) > 0 {
						keysToClean := make([]string, 0, len(tempKeyToFinalKeyMap))
						for tKey := range tempKeyToFinalKeyMap {
							keysToClean = append(keysToClean, tKey)
						}
						c.redisClient.Del(ctx, keysToClean...)
					}
					return fmt.Errorf("写入新详情到临时缓存失败: %w", execErr)
				}
				c.logger.Info("成功将聚合帖子详情写入临时Key区域", zap.Int("count", tempKeyWritesAttempted), zap.Int("marshalErrors", marshalErrorCountInStage1))
			} else if len(idsToFetchAndAggregate) > 0 {
				c.logger.Warn("有待缓存的帖子ID，但未能成功准备任何详情数据写入临时缓存（可能DB无数据或全部序列化失败）。",
					zap.Int("idsToFetchCount", len(idsToFetchAndAggregate)))
			}
		} else {
			c.logger.Info("从数据库未获取到任何需要聚合的新帖子详情数据。", zap.Int("requestedCount", len(idsToFetchAndAggregate)))
		}
	} else {
		c.logger.Info("没有新的帖子详情需要获取、聚合和缓存。")
	}

	// 5. 阶段二：删除不再热门的帖子详情缓存 (final keys)
	if len(finalKeysToDelete) > 0 {
		c.logger.Info("开始删除不再热门的帖子详情缓存", zap.Int("count", len(finalKeysToDelete)))
		pipe := c.redisClient.Pipeline()
		for _, keyToDel := range finalKeysToDelete {
			pipe.Del(ctx, keyToDel)
		}
		if _, execErr := pipe.Exec(ctx); execErr != nil {
			c.logger.Warn("Pipeline执行失败：删除不再热门的帖子详情时出错，部分旧缓存可能残留。",
				zap.Error(execErr), zap.Int("deleteKeyCount", len(finalKeysToDelete)))
		} else {
			c.logger.Info("成功删除不再热门的帖子详情缓存", zap.Int("count", len(finalKeysToDelete)))
		}
	}

	// 6. 阶段三：激活新的热门帖子详情缓存 (RENAME temp keys to final keys)
	if len(tempKeyToFinalKeyMap) > 0 {
		c.logger.Info("开始激活新的帖子详情缓存 (RENAME操作)", zap.Int("count", len(tempKeyToFinalKeyMap)))
		renamePipe := c.redisClient.Pipeline()
		for tempKey, finalKeyToRenameTo := range tempKeyToFinalKeyMap {
			renamePipe.Rename(ctx, tempKey, finalKeyToRenameTo)
		}
		_, execErr := renamePipe.Exec(ctx)
		if execErr != nil {
			c.logger.Error("Pipeline执行严重失败：RENAME临时Key到最终Key时出错。缓存状态可能不一致，部分新数据可能仍在临时区。",
				zap.Error(execErr), zap.Int("renameCount", len(tempKeyToFinalKeyMap)))
			return fmt.Errorf("RENAME临时缓存失败: %w", execErr)
		}
		c.logger.Info("成功激活新的帖子详情缓存", zap.Int("count", len(tempKeyToFinalKeyMap)))
	}

	duration := time.Since(startTime)
	c.logger.Info("完成同步热门帖子详情到 Redis 任务", zap.Duration("duration", duration))
	return nil
}
