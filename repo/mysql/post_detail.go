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
	// - 注意事项: 仅更新 content、price_per_unit 和 contact_qr_code 字段，避免修改无关字段
	UpdatePostDetail(ctx context.Context, postDetail *entities.PostDetail) error

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
		"content":        postDetail.Content,
		"price_per_unit": postDetail.PricePerUnit,
		"contact_info":   postDetail.ContactInfo,
	}).Error; err != nil {
		return err
	}
	return nil
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
	return nil
}
