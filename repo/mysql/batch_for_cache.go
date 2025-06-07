// File: repo/mysql/batch_for_cache.go
package mysql

import (
	"context"
	"fmt"
	"github.com/Xushengqwer/go-common/core"
	"strings"
	"sync"
	"time"

	"github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/models/entities"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PostBatchOperationsRepository defines the interface for batch database operations,
// primarily supporting tasks like syncing data with Redis or populating caches.
type PostBatchOperationsRepository interface {
	// BatchUpdatePostViewCounts 异步、并发地将 Redis 中的浏览量批量同步到 MySQL。
	// 设计目标是高吞吐量和容错性，允许在单个任务中处理大量更新，并记录但不中断因部分批次失败。
	BatchUpdatePostViewCounts(ctx context.Context, viewCounts map[uint64]int64) error

	// GetPostDetailsByPostIDs 批量获取帖子详情。
	// 主要用于为热门帖子缓存等场景提供数据源，通过单次查询减少数据库负载。
	GetPostDetailsByPostIDs(ctx context.Context, postIDs []uint64) ([]*entities.PostDetail, error)

	// GetPostsByIDs 根据 ID 列表批量检索帖子简略信息 (entities.Post)。
	// - 主要服务于需要一次性加载多个已知 ID 帖子的场景，例如填充 Redis 缓存或其他需要 Post 实体的场景。
	// - 使用 "WHERE id IN (...)" 进行查询。
	GetPostsByIDs(ctx context.Context, ids []uint64) ([]*entities.Post, error)

	// BatchGetPostDetailImages 检索多个帖子详情的图片。
	// 它接受一个 postDetailID 的切片，并返回一个映射（map），
	// 其中键是 postDetailID，值是 *entities.PostDetailImage 的切片。
	// 如果某个 postDetailID 没有图片，它仍然会存在于映射中，对应的值是一个空切片。
	BatchGetPostDetailImages(ctx context.Context, postDetailIDs []uint64) (map[uint64][]*entities.PostDetailImage, error)
}

type postBatchOperationsRepository struct {
	db          *gorm.DB
	logger      *core.ZapLogger
	viewSyncCfg config.ViewSyncConfig
}

// NewPostBatchOperationsRepository creates a new instance of PostBatchOperationsRepository.
func NewPostBatchOperationsRepository(db *gorm.DB, logger *core.ZapLogger, viewSyncCfg config.ViewSyncConfig) PostBatchOperationsRepository {
	return &postBatchOperationsRepository{db: db, logger: logger, viewSyncCfg: viewSyncCfg}
}

// updateItem 是一个内部结构体，用于在并发处理通道中传递 ID 和对应的浏览量。
type updateItem struct {
	ID        uint64
	ViewCount int64
}

// BatchUpdatePostViewCounts 实现了浏览量批量同步的核心逻辑。
//
// 使用场景:
// 由后台定时任务调用，将 Redis 中缓存的帖子浏览量 (viewCounts map)
// 定期、批量且并发地同步更新到 MySQL 的 post 表中。
//
// 核心机制:
// 1. 数据分批: 根据配置 `viewSyncCfg.BatchSize` 将大量更新分割成小批次。
// 2. 并发处理: 根据配置 `viewSyncCfg.ConcurrencyLevel` 启动 worker goroutine 池处理这些批次。
// 3. 数据库更新: 每个 worker 对其批次内的数据，通过 `processBatch` 方法构建单条 SQL (通常使用 CASE WHEN) 更新数据库。
//
// 设计目标:
// 高效同步数据，同时通过分批和并发控制数据库负载，保证服务稳定性。
// 允许部分批次失败（记录错误并聚合返回），以实现最终一致性。
func (r *postBatchOperationsRepository) BatchUpdatePostViewCounts(ctx context.Context, viewCounts map[uint64]int64) error {
	totalUpdates := len(viewCounts)
	if totalUpdates == 0 {
		r.logger.Info("BatchUpdatePostViewCounts: 没有需要更新的帖子浏览量，任务提前结束。")
		return nil // 如果没有数据，直接返回 nil 表示成功（无需操作）。
	}

	// --- 1. 加载并验证配置 ---
	batchSize := r.viewSyncCfg.BatchSize
	if batchSize <= 0 {
		batchSize = 500 // Fallback
		r.logger.Warn("BatchUpdatePostViewCounts: 配置 BatchSize 无效，使用默认值", zap.Int("defaultBatchSize", batchSize), zap.Int("configured", r.viewSyncCfg.BatchSize))
	}

	concurrencyLevel := r.viewSyncCfg.ConcurrencyLevel
	if concurrencyLevel <= 0 {
		concurrencyLevel = 1 // Fallback (顺序执行)
		r.logger.Warn("BatchUpdatePostViewCounts: 配置 ConcurrencyLevel 无效，使用默认值 1", zap.Int("defaultConcurrency", concurrencyLevel), zap.Int("configured", r.viewSyncCfg.ConcurrencyLevel))
	}

	// --- 2. 数据准备与日志记录 ---
	itemsToUpdate := make([]updateItem, 0, totalUpdates)
	for id, count := range viewCounts {
		itemsToUpdate = append(itemsToUpdate, updateItem{ID: id, ViewCount: count})
	}

	totalBatches := (totalUpdates + batchSize - 1) / batchSize
	r.logger.Info("BatchUpdatePostViewCounts: 开始并发批量更新",
		zap.Int("总数", totalUpdates),
		zap.Int("批大小", batchSize),
		zap.Int("并发数", concurrencyLevel),
		zap.Int("批次数", totalBatches),
	)

	// --- 3. 设置并发工作池 ---
	var wg sync.WaitGroup
	jobs := make(chan []updateItem, concurrencyLevel)
	results := make(chan error, totalBatches)
	overallStartTime := time.Now()

	// --- 4. 启动 Worker Goroutines ---
	r.logger.Info("BatchUpdatePostViewCounts: 启动 Worker", zap.Int("数量", concurrencyLevel))
	for i := 0; i < concurrencyLevel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r.logger.Debug("Worker 启动", zap.Int("workerID", workerID))
			for batch := range jobs {
				select {
				case <-ctx.Done():
					r.logger.Warn("上下文取消，Worker 停止处理", zap.Int("workerID", workerID), zap.Error(ctx.Err()))
					results <- fmt.Errorf("worker %d: context cancelled: %w", workerID, ctx.Err())
					continue
				default:
				}

				err := r.processBatch(ctx, batch, workerID)
				results <- err
			}
			r.logger.Debug("Worker 正常退出", zap.Int("workerID", workerID))
		}(i)
	}

	// --- 5. 启动分发任务 Goroutine ---
	go func() {
		defer func() {
			close(jobs)
			r.logger.Info("所有批次任务已发送完毕，jobs channel 已关闭。")
		}()

		for i := 0; i < totalUpdates; i += batchSize {
			end := i + batchSize
			if end > totalUpdates {
				end = totalUpdates
			}
			batchCopy := make([]updateItem, len(itemsToUpdate[i:end]))
			copy(batchCopy, itemsToUpdate[i:end])

			select {
			case <-ctx.Done():
				r.logger.Warn("上下文取消，停止分发更多批次任务。", zap.Error(ctx.Err()))
				return
			case jobs <- batchCopy:
			}
		}
	}()

	// --- 6. 启动收集结果 Goroutine ---
	var aggregatedErrors []error
	go func() {
		wg.Wait()
		close(results)
		r.logger.Info("所有 Worker 已完成处理，results channel 已关闭。")
	}()

	// --- 7. 收集并聚合结果 ---
	r.logger.Info("开始收集处理结果...")
	for err := range results {
		if err != nil {
			aggregatedErrors = append(aggregatedErrors, err)
		}
	}
	r.logger.Info("结果收集完毕。")

	// --- 8. 最终日志记录与返回 ---
	totalDuration := time.Since(overallStartTime)
	failedCount := len(aggregatedErrors)
	r.logger.Info("完成所有批次的帖子浏览量并发更新处理。",
		zap.Duration("总耗时", totalDuration),
		zap.Int("总批次数", totalBatches),
		zap.Int("失败批次数", failedCount),
	)

	if failedCount > 0 {
		var errorStrings []string
		for _, e := range aggregatedErrors {
			errorStrings = append(errorStrings, e.Error())
		}
		finalError := fmt.Errorf("并发批量更新过程中发生错误 (%d / %d 个批次失败): %s", failedCount, totalBatches, strings.Join(errorStrings, "; "))
		r.logger.Error("并发批量更新最终结果：失败", zap.Error(finalError))
		return finalError
	}

	r.logger.Info("并发批量更新最终结果：成功。")
	return nil
}

// processBatch 负责处理单个批次的数据库更新。
func (r *postBatchOperationsRepository) processBatch(ctx context.Context, batch []updateItem, workerID int) error {
	currentBatchSize := len(batch)

	var (
		ids          []uint64
		sqlCase      strings.Builder
		updateParams []interface{}
	)
	sqlCase.WriteString("CASE id ")
	for _, item := range batch {
		ids = append(ids, item.ID)
		sqlCase.WriteString("WHEN ? THEN ? ")
		updateParams = append(updateParams, item.ID, item.ViewCount)
	}
	sqlCase.WriteString("END")

	dbOperationStart := time.Now()
	err := r.db.WithContext(ctx).Model(&entities.Post{}).
		Where("id IN ?", ids).
		Update("view_count", gorm.Expr(sqlCase.String(), updateParams...)).Error
	dbDuration := time.Since(dbOperationStart)

	if err != nil {
		r.logger.Error("processBatch: 数据库更新批次失败",
			zap.Int("workerID", workerID),
			zap.Int("batchSize", currentBatchSize),
			zap.Duration("db耗时", dbDuration),
			zap.Error(err),
		)
		return fmt.Errorf("worker %d 处理批次 (大小 %d) 失败: %w", workerID, currentBatchSize, err)
	}

	r.logger.Debug("processBatch: 数据库更新批次成功",
		zap.Int("workerID", workerID),
		zap.Int("batchSize", currentBatchSize),
		zap.Duration("db耗时", dbDuration),
	)
	return nil
}

// GetPostDetailsByPostIDs 批量获取帖子详情
func (r *postBatchOperationsRepository) GetPostDetailsByPostIDs(ctx context.Context, postIDs []uint64) ([]*entities.PostDetail, error) {
	var postDetails []*entities.PostDetail

	if len(postIDs) == 0 {
		r.logger.Debug("GetPostDetailsByPostIDs: postIDs 为空，返回空列表。")
		return postDetails, nil
	}
	r.logger.Debug("GetPostDetailsByPostIDs: 开始查询帖子详情。", zap.Int("id数量", len(postIDs)))

	err := r.db.WithContext(ctx).
		Where("post_id IN ?", postIDs).
		Find(&postDetails).Error

	if err != nil {
		r.logger.Error("GetPostDetailsByPostIDs: 查询帖子详情失败。", zap.Error(err))
		return nil, err
	}

	r.logger.Debug("GetPostDetailsByPostIDs: 查询帖子详情成功。", zap.Int("找到数量", len(postDetails)))
	return postDetails, nil
}

// GetPostsByIDs 实现根据 ID 列表批量获取帖子 (entities.Post)。
func (r *postBatchOperationsRepository) GetPostsByIDs(ctx context.Context, ids []uint64) ([]*entities.Post, error) {
	var posts []*entities.Post

	if len(ids) == 0 {
		r.logger.Debug("GetPostsByIDs: ids 为空，返回空列表。")
		return posts, nil
	}
	r.logger.Debug("GetPostsByIDs: 开始查询帖子。", zap.Int("id数量", len(ids)))

	// GORM 的 Find 方法会自动处理软删除（如果模型中有 DeletedAt），并只返回存在的记录。
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&posts).Error; err != nil {
		r.logger.Error("GetPostsByIDs: 查询帖子失败。", zap.Error(err))
		return nil, err
	}

	r.logger.Debug("GetPostsByIDs: 查询帖子成功。", zap.Int("找到数量", len(posts)))
	return posts, nil
}

// BatchGetPostDetailImages 从数据库中批量检索多个帖子详情的图片。
// 此方法是为批量缓存预热或数据同步到Redis等场景设计的。
func (r *postBatchOperationsRepository) BatchGetPostDetailImages(ctx context.Context, postDetailIDs []uint64) (map[uint64][]*entities.PostDetailImage, error) {
	// 如果输入的帖子详情ID列表为空，则直接返回一个空的映射和nil错误。
	if len(postDetailIDs) == 0 {
		return make(map[uint64][]*entities.PostDetailImage), nil
	}

	var images []*entities.PostDetailImage
	// 使用 GORM 从数据库中查询所有 post_detail_id 在 postDetailIDs 切片中的图片记录。
	// entities.PostDetailImage 结构体定义了数据库表名 "post_detail_images"。
	// PostDetailID 字段对应数据库中的 "post_detail_id" 列。
	// 查询结果会按照 post_detail_id 和图片自身的 "order" 字段升序排列，以确保结果的顺序一致性。
	// "order" 字段需要用双引号括起来，以避免与SQL的ORDER BY关键字冲突（如果列名恰好是"order"）。
	if err := r.db.WithContext(ctx).
		Where("post_detail_id IN ?", postDetailIDs).
		Order("post_detail_id asc, \"order\" asc"). // 确保一致的排序
		Find(&images).Error; err != nil {
		// 如果查询过程中发生错误，例如数据库连接问题或SQL语法错误，
		// 将返回nil映射和具体的错误信息。
		// 实际项目中，这里可以加入更详细的日志记录或错误包装。
		// return nil, fmt.Errorf("BatchGetPostDetailImages: 查询帖子详情图片失败: %w", err)
		return nil, err
	}

	// 初始化一个映射，用于存储最终的结果。预估容量以提高效率。
	// 键是 int64 类型的帖子详情ID，值是 []*entities.PostDetailImage (图片实体指针的切片)。
	imagesMap := make(map[uint64][]*entities.PostDetailImage, len(postDetailIDs))

	// 遍历查询到的图片列表，将它们按照 PostDetailID 分组存入 imagesMap。
	for _, img := range images {
		// append 函数会自动处理 imagesMap[img.PostDetailID] 为 nil 的情况（即首次为该ID添加图片）。
		imagesMap[img.PostDetailID] = append(imagesMap[img.PostDetailID], img)
	}

	// 为了确保返回的映射中包含所有请求的 postDetailIDs（即使某些ID没有对应的图片），
	// 遍历原始的 postDetailIDs 列表。
	// 如果某个ID在 imagesMap 中不存在（即没有查询到任何图片），
	// 则为该ID在映射中创建一个条目，其值为一个空的图片切片。
	// 这样做可以方便调用方直接遍历 postDetailIDs 并从映射中取值，无需再次检查键是否存在。
	for _, id := range postDetailIDs {
		if _, ok := imagesMap[id]; !ok {
			imagesMap[id] = []*entities.PostDetailImage{} // 分配一个空切片
		}
	}

	// 返回构建好的图片映射和nil错误（表示操作成功）。
	return imagesMap, nil
}
