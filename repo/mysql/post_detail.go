package mysql

import (
	"context"
	"errors"
	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/post_service/models/entities"
	"gorm.io/gorm"
)

type PostDetailRepository interface {
	// CreatePostDetail 创建新帖子详情
	// - 意图: 将新的帖子详情信息插入数据库，用于记录帖子的详细信息
	// - 输入: ctx context.Context, postDetail *entities.PostDetail
	// - 输出: error
	// - 原生 SQL: INSERT INTO post_details (post_id, content, price_per_unit, author_id, author_avatar, author_username, contact_qr_code, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	CreatePostDetail(ctx context.Context, db *gorm.DB, postDetail *entities.PostDetail) error

	// GetPostDetailByPostID 根据PostID获取帖子详情
	// - 意图: 从数据库中检索指定PostID的帖子详情，用于查看帖子详细信息
	// - 输入: ctx context.Context, postID uint64
	// - 输出: *entities.PostDetail, error
	// - 原生 SQL: SELECT * FROM post_details WHERE post_id = ? AND deleted_at IS NULL
	// - 注意事项: 若帖子详情不存在，返回 ErrPostDetailNotFound
	GetPostDetailByPostID(ctx context.Context, postID uint64) (*entities.PostDetail, error)

	// UpdatePostDetail 更新帖子详情信息
	// - 意图: 更新数据库中指定帖子详情的内容、单价和联系方式，用于修改帖子详细信息
	// - 输入: ctx context.Context, postDetail *entities.PostDetail
	// - 输出: error
	// - 原生 SQL: UPDATE post_details SET content = ?, price_per_unit = ?, contact_qr_code = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL
	// - 注意事项: 仅更新 content、price_per_unit 和 contact_qr_code 字段，避免修改无关字段
	UpdatePostDetail(ctx context.Context, postDetail *entities.PostDetail) error

	// GetPostDetailsByPostIDs 批量获取帖子详情
	// - 意图: 根据帖子 ID 列表一次性查询多个帖子详情
	// - 场景: 支持服务，专门给redis调用的，在一个网络连接中批量加载帖子的详情到redis作为热门数据的缓存数据源，可以大量减轻数据库服务器的压力
	// - 输入: ctx context.Context 用于上下文控制, postIDs []uint64 帖子 ID 列表
	// - 输出: []*entities.PostDetail 查询到的帖子详情列表, error 数据库操作错误
	// - 原生 SQL: SELECT * FROM post_details WHERE post_id IN (...) AND deleted_at IS NULL
	// - 注意事项: 若某些 ID 对应的详情不存在，仍返回存在的记录，不视为错误
	GetPostDetailsByPostIDs(ctx context.Context, postIDs []uint64) ([]*entities.PostDetail, error)

	// DeletePostDetailByPostID 根据 PostID 软删除帖子详情
	// - 意图: 将指定 PostID 的帖子详情标记为已删除，用于逻辑删除帖子详情
	// - 输入: ctx context.Context, postID uint64
	// - 输出: error
	// - 原生 SQL: UPDATE post_details SET deleted_at = ? WHERE post_id = ? AND deleted_at IS NULL
	DeletePostDetailByPostID(ctx context.Context, db *gorm.DB, postID uint64) error
}

type postDetailRepository struct {
	db *gorm.DB // GORM 数据库实例
}

// NewPostDetailRepository 创建 PostDetailRepository 实例
func NewPostDetailRepository(db *gorm.DB) PostDetailRepository {
	return &postDetailRepository{db: db}
}

// CreatePostDetail 创建新帖子详情
func (r *postDetailRepository) CreatePostDetail(ctx context.Context, db *gorm.DB, postDetail *entities.PostDetail) error {
	// 使用传入的 db 对象执行数据库操作
	if err := db.WithContext(ctx).Create(postDetail).Error; err != nil {
		return err
	}
	return nil
}

// GetPostDetailByPostID 根据PostID获取帖子详情
func (r *postDetailRepository) GetPostDetailByPostID(ctx context.Context, postID uint64) (*entities.PostDetail, error) {
	// Step 1: 定义帖子详情实体变量用于接收查询结果
	var postDetail entities.PostDetail

	// Step 2: 使用 GORM 的 First 方法查询指定PostID的帖子详情
	err := r.db.WithContext(ctx).Where("post_id = ?", postID).First(&postDetail).Error
	if err != nil {
		// Step 3: 检查是否为记录未找到错误，若是则返回自定义错误
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, commonerrors.ErrRepoNotFound
		}
		return nil, err
	}
	return &postDetail, nil
}

// UpdatePostDetail 更新帖子详情信息
func (r *postDetailRepository) UpdatePostDetail(ctx context.Context, postDetail *entities.PostDetail) error {
	// Step 1: 使用 GORM 的 Updates 方法更新指定字段
	if err := r.db.WithContext(ctx).Model(postDetail).Updates(map[string]interface{}{
		"content":         postDetail.Content,
		"price_per_unit":  postDetail.PricePerUnit,
		"contact_qr_code": postDetail.ContactQRCode,
	}).Error; err != nil {
		return err
	}
	return nil
}

// GetPostDetailsByPostIDs 批量获取帖子详情
func (r *postDetailRepository) GetPostDetailsByPostIDs(ctx context.Context, postIDs []uint64) ([]*entities.PostDetail, error) {
	// Step 1: 定义帖子详情列表用于接收查询结果
	var postDetails []*entities.PostDetail

	if len(postIDs) == 0 {
		return postDetails, nil
	}

	// Step 2: 使用 GORM 的 Where 和 Find 方法批量查询帖子详情
	err := r.db.WithContext(ctx).
		Where("post_id IN ?", postIDs).
		Find(&postDetails).Error
	if err != nil {
		return nil, err
	}

	// Step 3: 返回查询结果，即使某些 ID 未找到记录也不返回错误
	return postDetails, nil
}

// DeletePostDetailByPostID 按 PostID 软删除帖子详情
// db 参数是执行此操作的数据库句柄
func (r *postDetailRepository) DeletePostDetailByPostID(ctx context.Context, db *gorm.DB, postID uint64) error {
	// 确保 entities.PostDetail 结构体中嵌入了 gorm.DeletedAt
	// 使用传入的 db 对象执行数据库操作
	result := db.WithContext(ctx).Where("post_id = ?", postID).Delete(&entities.PostDetail{})
	if result.Error != nil {
		return result.Error
	}
	// if resulted.RowsAffected == 0 {
	// 	return commonerrors.ErrRepoNotFound
	// }
	return nil
}
