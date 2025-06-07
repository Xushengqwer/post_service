package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt" // 需要导入 fmt
	"github.com/Xushengqwer/go-common/models/enums"
	"time"

	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/go-common/core" // 导入日志库
	"go.uber.org/zap"                       // 导入 zap
	"gorm.io/gorm"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/models/entities"
)

// PostAdminRepository 定义了帖子管理员相关的数据库操作接口。
// - 主要负责管理员后台对帖子数据的查询和状态修改。
type PostAdminRepository interface {
	// GetPostByID 根据主键 ID 获取单个帖子的简略信息。
	// - 主要用于管理员需要精确查找特定帖子时。
	// - 注意: 如果记录未找到，应返回明确的错误（如 commonerrors.ErrRepoNotFound）。
	GetPostByID(ctx context.Context, id uint64) (*entities.Post, error)

	// UpdatePostStatus 更新指定帖子的状态和可选的审核原因。
	// - 用于管理员审核帖子（通过/拒绝）或系统自动更新状态。
	// - reason (sql.NullString): 使用 sql.NullString 以区分 NULL 和空字符串。
	// - 注意: 如果记录未找到或已被软删除，应返回明确的错误。
	UpdatePostStatus(ctx context.Context, postID uint64, status enums.Status, reason sql.NullString) error

	// ListPostsByCondition 根据多种可选条件分页查询帖子列表。
	// - 服务于管理员后台的复杂查询和筛选需求。
	// - 输入 (req *dto.ListPostsByConditionRequest): 使用 DTO 封装查询条件，便于扩展。
	// - 输出: 返回帖子列表和满足条件的总记录数，用于分页展示。
	ListPostsByCondition(ctx context.Context, req *dto.ListPostsByConditionRequest) ([]*entities.Post, int64, error)

	// UpdateOfficialTag 更新指定帖子的官方标签。
	// - 允许管理员为帖子添加或修改官方认证等标签。
	// - 注意: 如果记录未找到或已被软删除，应返回明确的错误。
	UpdateOfficialTag(ctx context.Context, postID uint64, tag enums.OfficialTag) error
}

// postAdminRepository 是 PostAdminRepository 接口的 MySQL 实现。
type postAdminRepository struct {
	db     *gorm.DB        // GORM 数据库实例
	logger *core.ZapLogger // 日志记录器实例
}

// NewPostAdminRepository 是 postAdminRepository 的构造函数。
// - 通过依赖注入传入 db 和 logger。
func NewPostAdminRepository(db *gorm.DB, logger *core.ZapLogger) PostAdminRepository { // 添加 logger 参数
	return &postAdminRepository{
		db:     db,
		logger: logger, // 初始化 logger
	}
}

// GetPostByID 实现根据 ID 获取帖子的逻辑。
func (r *postAdminRepository) GetPostByID(ctx context.Context, id uint64) (*entities.Post, error) {
	var post entities.Post
	// 使用 GORM 的 First 方法，它在找到记录时填充 post，否则返回错误。
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&post).Error
	if err != nil {
		// 区分“记录未找到”和其他数据库错误。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 对于未找到的情况，返回预定义的错误，方便上层判断。
			r.logger.Warn("尝试获取不存在的帖子", zap.Uint64("id", id))
			return nil, commonerrors.ErrRepoNotFound
		}
		// 对于其他数据库错误，记录日志并返回原始错误。
		r.logger.Error("根据 ID 获取帖子失败", zap.Error(err), zap.Uint64("id", id))
		return nil, err
	}
	return &post, nil
}

// UpdatePostStatus 实现更新帖子状态和原因的逻辑。
func (r *postAdminRepository) UpdatePostStatus(ctx context.Context, postID uint64, status enums.Status, reason sql.NullString) error {
	// 准备需要更新的字段 map。
	// 使用 map 可以确保只更新指定的字段。
	updateData := map[string]interface{}{
		"status":       status,
		"updated_at":   time.Now(), // 总是更新修改时间
		"audit_reason": reason,     // 更新审核原因 (可以是 NULL)
	}

	// 执行更新操作，限制条件为 ID 匹配且未被软删除。
	result := r.db.WithContext(ctx).
		Model(&entities.Post{}).
		Where("id = ? AND deleted_at IS NULL", postID).
		Updates(updateData)

	// 处理 GORM 操作本身的错误。
	if result.Error != nil {
		r.logger.Error("更新帖子状态数据库出错", zap.Error(result.Error), zap.Uint64("postID", postID), zap.Any("status", status))
		return result.Error
	}
	// 检查是否有行受到影响。如果没有，说明帖子未找到或已被删除。
	if result.RowsAffected == 0 {
		r.logger.Warn("尝试更新不存在或已删除帖子的状态", zap.Uint64("postID", postID), zap.Any("status", status))
		return commonerrors.ErrRepoNotFound
	}
	// 记录更新成功（可选，Debug 级别）
	r.logger.Debug("成功更新帖子状态", zap.Uint64("postID", postID), zap.Any("status", status))
	return nil
}

// ListPostsByCondition 实现按条件分页查询帖子。
func (r *postAdminRepository) ListPostsByCondition(ctx context.Context, req *dto.ListPostsByConditionRequest) ([]*entities.Post, int64, error) {
	var posts []*entities.Post
	// Model(&entities.Post{}) 用于 GORM 知道基础查询针对哪个表，特别是 Count 操作需要。
	dbQuery := r.db.WithContext(ctx).Model(&entities.Post{}).Where("deleted_at IS NULL")

	// 优化：如果提供了精确的 ID，直接查询该 ID，忽略其他条件。
	if req.ID != nil {
		err := dbQuery.Where("id = ?", *req.ID).First(&posts).Error // GORM First 返回单个记录或错误
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				r.logger.Info("按条件查询帖子：未找到指定 ID", zap.Uint64p("id", req.ID))
				return nil, 0, nil // 未找到不算错误，返回空结果
			}
			r.logger.Error("按 ID 查询帖子失败", zap.Error(err), zap.Uint64p("id", req.ID))
			return nil, 0, err // 其他数据库错误
		}
		// 如果 First 成功，理论上只有一条记录
		if len(posts) == 0 { // GORM v2 Find 可能返回空切片
			var singlePost entities.Post
			err = r.db.WithContext(ctx).Where("id = ?", *req.ID).First(&singlePost).Error
			if err == nil {
				posts = append(posts, &singlePost)
			} // 如果这里还出错，则原始 err 处理会捕获
		}
		r.logger.Debug("按条件查询帖子：通过 ID 找到", zap.Uint64p("id", req.ID))
		return posts, 1, nil // 返回单条记录及总数 1
	}

	// --- 动态构建查询条件 ---
	// 使用 Where 方法链式添加条件。
	// 对于可选条件，先判断 DTO 中的字段是否为 nil。
	if req.Title != nil {
		dbQuery = dbQuery.Where("title LIKE ?", "%"+*req.Title+"%")
	}
	if req.AuthorUsername != nil {
		dbQuery = dbQuery.Where("author_username LIKE ?", "%"+*req.AuthorUsername+"%")
	}
	if req.Status != nil {
		dbQuery = dbQuery.Where("status = ?", *req.Status)
	}
	if req.OfficialTag != nil {
		dbQuery = dbQuery.Where("official_tag = ?", *req.OfficialTag)
	}
	// 处理范围查询
	if req.ViewCountMin != nil || req.ViewCountMax != nil {
		// 这里可以简化逻辑，因为 GORM 的 Where 能处理 nil 值（虽然显式检查更清晰）
		if req.ViewCountMin != nil {
			dbQuery = dbQuery.Where("view_count >= ?", *req.ViewCountMin)
		}
		if req.ViewCountMax != nil {
			dbQuery = dbQuery.Where("view_count <= ?", *req.ViewCountMax)
		}
	}

	// --- 处理排序 ---
	orderField := "created_at" // 默认排序字段
	if req.OrderBy == "updated_at" {
		orderField = "updated_at"
	}
	orderDirection := "ASC" // 默认升序
	if req.OrderDesc {
		orderDirection = "DESC" // 如果 DTO 要求降序
	}
	// 构建完整的 ORDER BY 子句
	orderClause := fmt.Sprintf("%s %s", orderField, orderDirection)

	// --- 执行 Count 查询 ---
	// 先计算总数，此时不应用 Limit 和 Offset，但应用 Where 条件。
	var total int64
	// GORM 的 Count 会自动忽略 Order 子句。
	if err := dbQuery.Count(&total).Error; err != nil {
		r.logger.Error("按条件查询帖子计数失败", zap.Error(err))
		return nil, 0, err
	}

	// 如果总数为 0，无需执行后续的 Find 查询。
	if total == 0 {
		r.logger.Debug("按条件查询帖子：未找到匹配记录")
		return posts, 0, nil // 返回空列表和总数 0
	}

	// --- 执行分页数据查询 ---
	// 计算偏移量。Page 从 1 开始。
	offset := (req.Page - 1) * req.PageSize
	// 应用排序、Limit 和 Offset，执行查询。
	if err := dbQuery.Order(orderClause).Limit(req.PageSize).Offset(offset).Find(&posts).Error; err != nil {
		r.logger.Error("按条件查询帖子分页数据失败", zap.Error(err))
		return nil, 0, err
	}

	r.logger.Debug("按条件查询帖子成功", zap.Int("page", req.Page), zap.Int("pageSize", req.PageSize), zap.Int64("total", total))
	return posts, total, nil // 返回查询结果和总数
}

// UpdateOfficialTag 实现更新帖子官方标签的逻辑。
func (r *postAdminRepository) UpdateOfficialTag(ctx context.Context, postID uint64, tag enums.OfficialTag) error {
	updateData := map[string]interface{}{
		"official_tag": tag,
		"updated_at":   time.Now(),
	}

	result := r.db.WithContext(ctx).
		Model(&entities.Post{}).
		Where("id = ? AND deleted_at IS NULL", postID).
		Updates(updateData)

	if result.Error != nil {
		r.logger.Error("更新官方标签数据库出错", zap.Error(result.Error), zap.Uint64("postID", postID), zap.Any("tag", tag))
		return result.Error
	}
	if result.RowsAffected == 0 {
		r.logger.Warn("尝试更新不存在或已删除帖子的官方标签", zap.Uint64("postID", postID), zap.Any("tag", tag))
		return commonerrors.ErrRepoNotFound
	}
	r.logger.Debug("成功更新帖子官方标签", zap.Uint64("postID", postID), zap.Any("tag", tag))
	return nil
}
