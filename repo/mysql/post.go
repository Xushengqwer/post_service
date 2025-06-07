package mysql

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/go-common/models/enums"
	"github.com/Xushengqwer/post_service/models/dto"
	"go.uber.org/zap"
	"time" // 用于更新时间戳

	"gorm.io/gorm"

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

	// UpdatePost 更新指定帖子的核心信息。
	// - 可选更新 Title, AuthorID, AuthorAvatar, AuthorUsername。
	// - 传入 nil 表示不更新对应字段。
	// - 总是会自动更新帖子的修改时间 (updated_at)。
	UpdatePost(ctx context.Context, postID uint64, title *string, authorID *string, authorAvatar *string, authorUsername *string) error

	// GetPostsByUserIDCursor 实现用户帖子列表的游标分页查询。
	// - 设计为降序（ID越大越新），适用于“用户个人主页”等场景展示最新帖子。
	// - cursor (*uint64): 使用指针类型是为了区分“首次加载”（nil）和“从某个ID之后加载”。
	// - 返回 nextCursor (*uint64): 下一页的起始ID，如果为 nil 表示没有更多数据。
	GetPostsByUserIDCursor(ctx context.Context, userID string, cursor *uint64, pageSize int) ([]*entities.Post, *uint64, error)

	// GetPostsByTimeline 实现按时间线、条件筛选和游标分页查询帖子列表。
	// - 使用 TimelineQueryDTO 封装所有查询参数。
	// - 返回 ([]*entities.Post, *time.Time, *uint64, error): 帖子列表, 下一页游标时间, 下一页游标ID, 错误。
	GetPostsByTimeline(ctx context.Context, params *dto.TimelineQueryDTO) ([]*entities.Post, *time.Time, *uint64, error)

	// GetUserPostsByConditions 分页查询指定用户发布的帖子列表，支持多种条件筛选。
	// - authorID: 必需，指定用户ID。
	// - officialTag (*enums.OfficialTag): 可选，按官方标签筛选。
	// - title (*string): 可选，按标题模糊搜索。
	// - status (*enums.Status): 可选，按帖子审核状态筛选。
	// - offset (int): 分页偏移量。
	// - limit (int): 每页数量。
	// - 返回: 帖子列表 ([]*entities.Post), 符合条件的总记录数 (int64), 错误 (error)。
	GetUserPostsByConditions(ctx context.Context, authorID string, officialTag *enums.OfficialTag, title *string, status *enums.Status, offset, limit int) ([]*entities.Post, int64, error)

	// GetPostByID 根据单个 ID 检索帖子信息。
	// - 用于需要获取指定帖子基础信息的场景。
	// - 如果未找到帖子，应返回 commonerrors.ErrRepoNotFound 错误。
	GetPostByID(ctx context.Context, id uint64) (*entities.Post, error)

	// DeletePost 对指定帖子执行软删除。
	// - 软删除是通过 GORM 的约定（填充 deleted_at 字段）实现的，数据本身仍在数据库中。
	// - 适用于用户下架或管理员删除帖子的场景，保留数据可追溯。
	DeletePost(ctx context.Context, db *gorm.DB, id uint64) error
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

// UpdatePost 实现帖子核心信息 (Title, AuthorID, AuthorAvatar, AuthorUsername) 的更新。
// 参数为指针类型，如果传入 nil，则对应字段不会被更新。
func (r *postRepository) UpdatePost(ctx context.Context, postID uint64, title *string, authorID *string, authorAvatar *string, authorUsername *string) error {
	updateMap := make(map[string]interface{})

	if title != nil {
		updateMap["title"] = *title
	}
	if authorID != nil {
		updateMap["author_id"] = *authorID
	}
	if authorAvatar != nil {
		updateMap["author_avatar"] = *authorAvatar
	}
	if authorUsername != nil {
		updateMap["author_username"] = *authorUsername
	}

	// 检查是否有任何字段需要更新。
	if len(updateMap) == 0 {
		r.logger.Info("没有提供任何有效的字段来更新帖子 (所有可选参数均为nil)",
			zap.Uint64("postID", postID),
		)
		return nil
	}

	// 总是更新 updated_at 字段
	updateMap["updated_at"] = time.Now()

	result := r.db.WithContext(ctx).
		Model(&entities.Post{}).
		Where("id = ? AND deleted_at IS NULL", postID).
		Updates(updateMap)

	if result.Error != nil {
		r.logger.Error("更新帖子数据库操作失败",
			zap.Error(result.Error),
			zap.Uint64("postID", postID),
			zap.Any("updateData", updateMap), // 记录实际尝试更新的字段
		)
		return result.Error
	}

	if result.RowsAffected == 0 {
		r.logger.Warn("尝试更新帖子但未找到记录或记录已被删除",
			zap.Uint64("postID", postID),
		)
		return commonerrors.ErrRepoNotFound
	}

	r.logger.Info("帖子信息更新成功", zap.Uint64("postID", postID), zap.Any("updatedFields", updateMap))
	return nil
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

// GetPostsByTimeline 实现按时间线、条件筛选和游标分页查询帖子列表（使用 DTO）。
func (r *postRepository) GetPostsByTimeline(ctx context.Context, params *dto.TimelineQueryDTO) ([]*entities.Post, *time.Time, *uint64, error) {
	var posts []*entities.Post // 用于存储查询结果

	// 检查 PageSize 是否有效
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
		r.logger.Warn("GetPostsByTimeline 接收到的 PageSize 无效，使用默认值",
			zap.Int("receivedPageSize", params.PageSize),
			zap.Int("defaultPageSize", pageSize),
		)
	}

	// 构建基础查询：只看已通过审核 (Approved) 的帖子
	query := r.db.WithContext(ctx).
		Model(&entities.Post{}).
		Where("status = ?", enums.Approved)

	// 应用筛选条件 (检查指针是否为 nil)
	if params.OfficialTag != nil {
		query = query.Where("official_tag = ?", *params.OfficialTag)
	}
	if params.Title != nil {
		// 只有当 Title 不为 nil 时才添加 WHERE 条件
		query = query.Where("title LIKE ?", "%"+*params.Title+"%")
	}
	if params.AuthorUsername != nil {
		// 只有当 AuthorUsername 不为 nil 时才添加 WHERE 条件
		query = query.Where("author_username LIKE ?", "%"+*params.AuthorUsername+"%")
	}

	// 应用游标分页条件 (检查指针是否为 nil)
	if params.LastCreatedAt != nil && params.LastPostID != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", *params.LastCreatedAt, *params.LastCreatedAt, *params.LastPostID)
	}

	// 定义排序：首先按创建时间降序，然后按 ID 降序
	query = query.Order("created_at DESC").Order("id DESC")

	// 查询 pageSize + 1 条记录
	err := query.Limit(pageSize + 1).Find(&posts).Error
	if err != nil {
		r.logger.Error("按时间线获取帖子列表数据库查询失败 (使用 DTO)",
			zap.Error(err),
			zap.Any("queryParams", params), // 直接记录整个 DTO (确保 DTO 是可序列化的或有 String 方法)
		)
		return nil, nil, nil, err
	}

	// 准备下一页的游标
	var nextCreatedAt *time.Time
	var nextPostID *uint64

	// 检查实际返回的帖子数量是否超过请求的 pageSize。
	if len(posts) > pageSize {
		lastPostInPage := posts[pageSize-1]
		nextCreatedAt = &lastPostInPage.CreatedAt
		nextPostID = &lastPostInPage.ID
		posts = posts[:pageSize] // 截断结果
	}

	// 返回当前页数据和下一页游标
	return posts, nextCreatedAt, nextPostID, nil
}

// GetUserPostsByConditions 分页查询指定用户发布的帖子列表，支持多种条件筛选。
func (r *postRepository) GetUserPostsByConditions(ctx context.Context, authorID string, officialTag *enums.OfficialTag, title *string, status *enums.Status, offset, limit int) ([]*entities.Post, int64, error) {
	var posts []*entities.Post // 用于存储查询结果
	var totalCount int64       // 用于存储符合条件的总记录数

	// --- 构建基础查询 ---
	// 始终基于当前用户ID进行查询
	query := r.db.WithContext(ctx).Model(&entities.Post{}).Where("author_id = ?", authorID)
	countQuery := r.db.WithContext(ctx).Model(&entities.Post{}).Where("author_id = ?", authorID) // 用于计数的查询

	// --- 应用筛选条件 ---
	if officialTag != nil {
		query = query.Where("official_tag = ?", *officialTag)
		countQuery = countQuery.Where("official_tag = ?", *officialTag)
	}
	if title != nil && *title != "" { // 确保指针不为nil且字符串非空
		query = query.Where("title LIKE ?", "%"+*title+"%")
		countQuery = countQuery.Where("title LIKE ?", "%"+*title+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
		countQuery = countQuery.Where("status = ?", *status)
	}

	// --- 执行计数查询 ---
	// 在应用所有筛选条件后，但在应用分页和排序之前执行计数
	if err := countQuery.Count(&totalCount).Error; err != nil {
		r.logger.Error("获取用户帖子列表：计数查询失败",
			zap.Error(err),
			zap.String("authorID", authorID),
			zap.Any("officialTag", officialTag),
			zap.Any("title", title),
			zap.Any("status", status),
		)
		return nil, 0, fmt.Errorf("计数用户帖子失败: %w", err)
	}

	// 如果总数为0，无需执行后续的列表查询
	if totalCount == 0 {
		return posts, 0, nil // 返回空列表和总数0
	}

	// --- 应用排序和分页到列表查询 ---
	query = query.Order("created_at DESC").Order("id DESC") // 按时间线倒序
	query = query.Offset(offset).Limit(limit)               // 应用分页

	// --- 执行列表查询 ---
	if err := query.Find(&posts).Error; err != nil {
		r.logger.Error("获取用户帖子列表：列表查询失败",
			zap.Error(err),
			zap.String("authorID", authorID),
			zap.Any("officialTag", officialTag),
			zap.Any("title", title),
			zap.Any("status", status),
			zap.Int("offset", offset),
			zap.Int("limit", limit),
		)
		return nil, 0, fmt.Errorf("查询用户帖子列表失败: %w", err)
	}

	// 返回查询到的帖子列表和总记录数
	return posts, totalCount, nil
}

// GetPostByID 实现根据单个 ID 获取帖子。
func (r *postRepository) GetPostByID(ctx context.Context, id uint64) (*entities.Post, error) {
	var post entities.Post // 初始化一个空的帖子实体

	// 使用 GORM 的 First 方法根据主键查询。
	// First 会自动添加 "WHERE id = ?" 和 "LIMIT 1" 条件。
	// 它在找到记录时填充 post，如果未找到则返回 gorm.ErrRecordNotFound。
	err := r.db.WithContext(ctx).First(&post, id).Error

	if err != nil {
		// 检查错误是否为 GORM 的“未找到记录”错误。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 如果是，记录警告并返回我们自定义的 ErrRepoNotFound。
			r.logger.Warn("根据 ID 获取帖子未找到", zap.Uint64("postID", id), zap.Error(err))
			return nil, commonerrors.ErrRepoNotFound
		}
		// 如果是其他类型的数据库错误，记录错误并返回。
		r.logger.Error("根据 ID 获取帖子数据库查询失败", zap.Uint64("postID", id), zap.Error(err))
		return nil, err
	}

	// 如果没有错误，表示成功找到了帖子，返回帖子实体和 nil 错误。
	return &post, nil
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
