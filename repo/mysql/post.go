package mysql

import (
	"context"
	"fmt"
	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/go-common/models/enums"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time" // 用于更新时间戳

	"gorm.io/gorm"

	"github.com/Xushengqwer/post_service/constant"        // 引入常量，如 SyncBatchSize
	"github.com/Xushengqwer/post_service/models/entities" // 引入数据库实体定义
	// 建议: 如果使用自定义错误，在这里导入
	// "github.com/Xushengqwer/go-common/commonerrors"
)

// PostRepository 定义了帖子数据在 MySQL 中的持久化操作接口。
// 接口的设计旨在将数据访问逻辑与业务逻辑（服务层）解耦。
type PostRepository interface {
	// CreatePost 持久化一个新的帖子记录。
	// - 这是帖子生命周期的起点，对应用户发布帖子的操作。
	CreatePost(ctx context.Context, db *gorm.DB, post *entities.Post) error

	// UpdatePostTitle 仅更新指定帖子的标题。
	// - 允许用户修改已发布帖子的标题，同时自动更新修改时间。
	// - 使用 map[string]interface{} 进行更新是为了精确控制只修改 title 和 updated_at 字段。
	UpdatePostTitle(ctx context.Context, postID uint64, title string) error

	// GetPostsByUserIDCursor 实现用户帖子列表的游标分页查询。
	// - 设计为降序（ID越大越新），适用于“用户个人主页”等场景展示最新帖子。
	// - cursor (*uint64): 使用指针类型是为了区分“首次加载”（nil）和“从某个ID之后加载”。
	// - 返回 nextCursor (*uint64): 下一页的起始ID，如果为 nil 表示没有更多数据。
	GetPostsByUserIDCursor(ctx context.Context, userID string, cursor *uint64, pageSize int) ([]*entities.Post, *uint64, error)

	// GetPostsByIDs 根据 ID 列表批量检索帖子简略信息。
	// - 主要服务于需要一次性加载多个已知 ID 帖子的场景，例如填充 Redis 缓存。
	// - 使用 "WHERE id IN (...)" 进行查询。
	GetPostsByIDs(ctx context.Context, ids []uint64) ([]*entities.Post, error)

	// DeletePost 对指定帖子执行软删除。
	// - 软删除是通过 GORM 的约定（填充 deleted_at 字段）实现的，数据本身仍在数据库中。
	// - 适用于用户下架或管理员删除帖子的场景，保留数据可追溯。
	DeletePost(ctx context.Context, db *gorm.DB, id uint64) error

	// BatchUpdateViewCount 批量、增量地将 Redis 中的浏览量同步到 MySQL。
	// - 采用 "INSERT ... ON DUPLICATE KEY UPDATE" (通过 clause.OnConflict 实现) 以提高效率。
	// - 分批处理是为了避免单次更新数据量过大对数据库造成压力。
	// - 设计为无事务、允许部分批次失败（下次同步会覆盖），以简化逻辑并接受最终一致性。
	// - viewCounts (map[uint64]int64): Key 是帖子ID，Value 是最新的总浏览量。
	// - 返回值 (error): 设计为始终返回 nil，因为单个批次的失败不应阻塞整个同步任务，错误应通过日志记录。
	BatchUpdateViewCount(ctx context.Context, viewCounts map[uint64]int64) error
}

// postRepository 是 PostRepository 接口针对 MySQL 的具体实现。
type postRepository struct {
	db     *gorm.DB        // GORM 数据库实例
	logger *core.ZapLogger // 新增：日志记录器实例
}

// NewPostRepository 是 postRepository 的构造函数。
func NewPostRepository(db *gorm.DB, logger *core.ZapLogger) PostRepository { // 增加 logger 参数
	return &postRepository{
		db:     db,
		logger: logger, // 初始化 logger 字段
	}
}

// CreatePost 实现帖子的数据库插入操作。
// 注意：方法签名已修改，增加了 db *gorm.DB 参数。
func (r *postRepository) CreatePost(ctx context.Context, db *gorm.DB, post *entities.Post) error {
	// 使用传入的 db 对象（在这里即为事务对象 tx）执行数据库操作。
	// GORM 会自动处理 BaseModel 或 gorm.Model 中的 CreatedAt 和 UpdatedAt 字段。
	if err := db.WithContext(ctx).Create(post).Error; err != nil {
		// 在仓库层，通常直接返回数据库错误，由服务层决定如何处理或包装。
		return err
	}
	// 创建成功后，post 对象会包含 GORM 自动生成的 ID 和时间戳。
	return nil
}

// UpdatePostTitle 实现帖子标题的更新。
func (r *postRepository) UpdatePostTitle(ctx context.Context, postID uint64, title string) error {
	// 使用 Model(&entities.Post{}) 指定操作的表。
	// Where 子句确保只更新指定 ID 且未被软删除的记录。
	// Updates 使用 map 只更新指定的字段，避免意外修改其他字段。
	// 显式更新 updated_at 是良好实践，即使 GORM 可能在某些情况下自动更新。
	result := r.db.WithContext(ctx).
		Model(&entities.Post{}).
		Where("id = ? AND deleted_at IS NULL", postID).
		Updates(map[string]interface{}{
			"title":      title,
			"updated_at": time.Now(),
		})

	// 检查 GORM 操作本身是否出错。
	if result.Error != nil {
		r.logger.Error("更新帖子标题数据库出错", zap.Error(result.Error), zap.Uint64("postID", postID))
		return result.Error
	}
	// 检查实际影响的行数。如果为 0，说明没有找到匹配的记录（可能 ID 不存在或已被删除）。
	if result.RowsAffected == 0 {
		r.logger.Warn("尝试更新不存在或已删除的帖子标题", zap.Uint64("postID", postID))
		// 返回一个错误，表明未找到要更新的记录。
		// 使用预定义的错误类型（如 commonerrors.ErrRepoNotFound）以便上层判断。
		return commonerrors.ErrRepoNotFound
	}

	return nil // 表示更新成功
}

// GetPostsByUserIDCursor 实现游标方式获取用户帖子。
func (r *postRepository) GetPostsByUserIDCursor(ctx context.Context, userID string, cursor *uint64, pageSize int) ([]*entities.Post, *uint64, error) {
	var posts []*entities.Post // 用于存储查询结果

	// 构建基础查询：指定用户、只看已通过审核 (Approved) 的帖子、按 ID 降序排序。
	query := r.db.WithContext(ctx).
		Where("author_id = ?", userID).
		Where("status = ?", enums.Approved).
		Order("id DESC")

	// 如果提供了 cursor (非首次加载)，则只查询 ID 小于 cursor 的记录。
	// 使用指针判断 cursor 是否被提供。
	if cursor != nil {
		query = query.Where("id < ?", *cursor)
	}

	// 查询 pageSize + 1 条记录，目的是判断是否还有下一页。
	// 如果查出的记录数 > pageSize，说明存在下一页。
	err := query.Limit(pageSize + 1).Find(&posts).Error
	if err != nil {
		// 如果查询本身出错（如数据库连接问题），直接返回错误。
		return nil, nil, err
	}

	var nextCursor *uint64 // 准备下一页的游标
	// 检查实际返回的帖子数量是否超过请求的 pageSize。
	if len(posts) > pageSize {
		// 如果超过，说明有下一页。
		// 将实际返回的列表截断为 pageSize。
		// 将最后一条记录的 ID (posts[pageSize-1].ID) 作为下一页的 cursor。
		// 注意：posts 此时包含 pageSize+1 条记录。
		nextCursor = &posts[pageSize-1].ID
		posts = posts[:pageSize]
	}
	// 如果 len(posts) <= pageSize，说明没有更多数据了，nextCursor 保持为 nil。

	return posts, nextCursor, nil // 返回当前页数据和下一页游标
}

// GetPostsByIDs 实现根据 ID 列表批量获取帖子。
func (r *postRepository) GetPostsByIDs(ctx context.Context, ids []uint64) ([]*entities.Post, error) {
	var posts []*entities.Post // 初始化为空切片

	// 处理空 ID 列表的边界情况，避免向数据库发送空的 "IN ()" 查询。
	if len(ids) == 0 {
		return posts, nil
	}

	// 使用 GORM 的 Where 和 IN 操作符执行批量查询。
	// Find 方法会将结果填充到 posts 切片中。
	// 如果某些 ID 在数据库中不存在，它们不会出现在结果中，Find 不会报错。
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&posts).Error; err != nil {
		// 返回数据库查询过程中可能发生的其他错误（如连接问题）。
		return nil, err
	}

	return posts, nil // 返回查询到的帖子列表
}

// DeletePost 实现帖子的软删除
// db 参数是执行此操作的数据库句柄 (可以是普通连接，也可以是事务 tx)
func (r *postRepository) DeletePost(ctx context.Context, db *gorm.DB, id uint64) error {
	// 确保 entities.Post 结构体中嵌入了 gorm.DeletedAt 以支持软删除
	// 使用传入的 db 对象执行数据库操作
	result := db.WithContext(ctx).Delete(&entities.Post{}, id)
	if result.Error != nil {
		return result.Error
	}

	// 可选：如果业务逻辑要求“删除不存在的记录”是一个需要特殊处理的错误，
	// 而不是静默成功 (GORM 默认行为)，可以在这里检查 RowsAffected。
	// if result.RowsAffected == 0 {
	//    return commonerrors.ErrRepoNotFound // 返回自定义的未找到错误
	// }
	return nil
}

// updateItem 用于在 BatchUpdateViewCount 内部传递待更新的数据
type updateItem struct {
	ID        uint64
	ViewCount int64
}

// BatchUpdateViewCount 批量更新帖子浏览量 (并发分批处理, 无显式事务, 接受 map 作为输入)
func (r *postRepository) BatchUpdateViewCount(ctx context.Context, viewCounts map[uint64]int64) error {
	totalUpdates := len(viewCounts)
	if totalUpdates == 0 {
		r.logger.Info("BatchUpdateViewCount: 没有需要更新的帖子浏览量")
		return nil
	}

	// --- 配置和数据准备 ---
	batchSize := constant.SyncBatchSize
	if batchSize <= 0 {
		batchSize = 500 // Fallback
		r.logger.Warn("BatchUpdateViewCount: 常量 SyncBatchSize 配置无效或为零，使用默认值", zap.Int("defaultBatchSize", batchSize))
	}

	concurrencyLevel := constant.ViewSyncConcurrency
	if concurrencyLevel <= 0 {
		concurrencyLevel = 1 // 至少为 1 (顺序执行)
		r.logger.Warn("BatchUpdateViewCount: 常量 ViewSyncConcurrency 配置无效或为零，使用默认值 1 (顺序执行)", zap.Int("defaultConcurrency", concurrencyLevel))
	}

	r.logger.Info("BatchUpdateViewCount: 开始并发批量更新帖子浏览量",
		zap.Int("待更新总数", totalUpdates),
		zap.Int("批次大小", batchSize),
		zap.Int("并发数", concurrencyLevel),
	)

	itemsToUpdate := make([]updateItem, 0, totalUpdates)
	for id, count := range viewCounts {
		if id == 0 {
			r.logger.Warn("BatchUpdateViewCount: 发现无效的帖子ID (0)，已跳过", zap.Int64("viewCount", count))
			continue
		}
		itemsToUpdate = append(itemsToUpdate, updateItem{ID: id, ViewCount: count})
	}

	totalValidItems := len(itemsToUpdate)
	if totalValidItems == 0 {
		r.logger.Info("BatchUpdateViewCount: 过滤无效ID后，没有需要更新的帖子")
		return nil
	}
	totalBatches := (totalValidItems + batchSize - 1) / batchSize
	r.logger.Info("BatchUpdateViewCount: 过滤无效ID后，实际待更新帖子数",
		zap.Int("有效数量", totalValidItems),
		zap.Int("总批次数", totalBatches),
	)

	// --- 并发控制与任务分发 ---
	var wg sync.WaitGroup
	jobs := make(chan []updateItem, concurrencyLevel)
	results := make(chan error, totalBatches) // 缓冲设为总批次数

	overallStartTime := time.Now() // 用于计算总耗时

	// --- 启动 Worker Goroutines ---
	r.logger.Info("BatchUpdateViewCount: 启动 Worker Goroutines", zap.Int("数量", concurrencyLevel))
	for i := 0; i < concurrencyLevel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			r.logger.Info("Worker 启动", zap.Int("workerID", workerID))
			for batch := range jobs {
				// 检查上下文是否已取消
				select {
				case <-ctx.Done():
					r.logger.Warn("上下文取消，Worker 停止处理", zap.Int("workerID", workerID), zap.Error(ctx.Err()))
					results <- fmt.Errorf("worker %d: context cancelled: %w", workerID, ctx.Err())
					// 继续循环，让 range jobs 自然结束（当 jobs 关闭时）
					continue
				default:
					// 继续处理批次
				}

				// 再次检查，确保在收到批次后上下文没有立即取消
				if ctx.Err() != nil {
					// 如果上下文已取消，不再处理收到的批次，但需要发送一个错误信号
					results <- fmt.Errorf("worker %d: context cancelled before processing batch: %w", workerID, ctx.Err())
					continue
				}

				// *** 已移除 worker 内部多余的 batchStartTime := time.Now() ***
				err := r.processBatch(ctx, batch, workerID) // 调用处理函数
				results <- err                              // 发送结果 (nil 或 error)
			}
			r.logger.Info("Worker 正常退出 (jobs channel closed)", zap.Int("workerID", workerID))
		}(i)
	}

	// --- 分发任务 Goroutine ---
	go func() {
		defer func() {
			close(jobs) // 发送完所有任务后关闭 jobs channel
			r.logger.Info("所有批次任务已发送完毕，jobs channel 已关闭")
		}()
		for i := 0; i < totalValidItems; i += batchSize {
			end := i + batchSize
			if end > totalValidItems {
				end = totalValidItems
			}
			batchCopy := make([]updateItem, len(itemsToUpdate[i:end]))
			copy(batchCopy, itemsToUpdate[i:end])

			// 发送前检查上下文
			select {
			case <-ctx.Done():
				r.logger.Warn("上下文取消，停止分发更多批次任务", zap.Error(ctx.Err()))
				return // 退出分发 goroutine
			case jobs <- batchCopy:
			}
		}
	}()

	// --- 收集结果 Goroutine ---
	var aggregatedErrors []error
	go func() {
		defer close(results) // 确保 results channel 被关闭
		wg.Wait()
		r.logger.Info("所有 Worker 已完成处理，results channel 即将关闭")
	}()

	r.logger.Info("开始收集处理结果...")
	for err := range results {
		if err != nil {
			aggregatedErrors = append(aggregatedErrors, err)
		}
	}
	r.logger.Info("结果收集完毕")

	// --- 完成与返回 ---
	totalDuration := time.Since(overallStartTime) // 使用 overallStartTime 计算总耗时
	r.logger.Info("完成所有批次的帖子浏览量并发更新处理",
		zap.Duration("总耗时", totalDuration),
		zap.Int("总批次数", totalBatches),
		zap.Int("失败批次数", len(aggregatedErrors)),
	)

	if len(aggregatedErrors) > 0 {
		var errorStrings []string
		for _, e := range aggregatedErrors {
			errorStrings = append(errorStrings, e.Error())
		}
		finalError := fmt.Errorf("并发批量更新过程中发生错误 (%d 个批次失败): %s", len(aggregatedErrors), strings.Join(errorStrings, "; "))
		r.logger.Error("并发批量更新最终结果：失败", zap.Error(finalError))
		return finalError
	}

	r.logger.Info("并发批量更新最终结果：成功")
	return nil
}

// processBatch 私有辅助方法
func (r *postRepository) processBatch(ctx context.Context, batch []updateItem, workerID int) error {
	currentBatchSize := len(batch)
	if currentBatchSize == 0 {
		r.logger.Debug("processBatch: Worker 收到空批次，跳过", zap.Int("workerID", workerID))
		return nil
	}

	// 构建 SQL
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

	// 执行数据库更新
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
