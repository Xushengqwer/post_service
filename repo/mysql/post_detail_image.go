package mysql

import (
	"context"
	// "errors" // 根据实际使用情况决定是否保留
	// "github.com/Xushengqwer/go-common/commonerrors" // 假设这是你的通用错误包
	"github.com/Xushengqwer/post_service/models/entities" // 确保这里的路径与你的项目结构一致
	"gorm.io/gorm"
)

// PostDetailImageRepository 定义了与 post_detail_images 表交互的接口。
type PostDetailImageRepository interface {
	// CreateImage 创建单个帖子详情图片条目。
	// - 意图: 向数据库插入单个图片的元数据。
	// - 输入: ctx context.Context, db *gorm.DB (用于事务操作), image *entities.PostDetailImage
	// - 输出: error
	// - 注意: 服务层应确保 image 对象中的 PostDetailID, ImageURL, DisplayOrder 等字段已正确填充。
	CreateImage(ctx context.Context, db *gorm.DB, image *entities.PostDetailImage) error

	// BatchCreatePostDetailImages 创建多个新的帖子详情图片条目。
	// - 意图: 高效地插入与帖子详情关联的多张图片的元数据。
	// - 输入: ctx context.Context, db *gorm.DB (用于事务操作), images []*entities.PostDetailImage
	// - 输出: error
	// - 原生 SQL (概念): INSERT INTO post_detail_images (post_id, image_url, display_order, created_at, updated_at) VALUES (...), (...), ...
	BatchCreatePostDetailImages(ctx context.Context, db *gorm.DB, images []*entities.PostDetailImage) error

	// GetImageByID 根据图片ID获取图片详情。
	// - 意图: 获取特定ID的图片元数据。
	// - 输入: ctx context.Context, imageID uint (假设BaseModel中的ID类型为uint)
	// - 输出: *entities.PostDetailImage, error
	// - 注意: 如果未找到，GORM会返回 gorm.ErrRecordNotFound。
	GetImageByID(ctx context.Context, imageID uint) (*entities.PostDetailImage, error)

	// GetImagesByPostDetailID 检索与给定 postDetailID 关联的所有图片。
	// - 意图: 获取特定帖子详情的所有图片 URL 及其顺序。
	// - 输入: ctx context.Context, postDetailID uint
	// - 输出: []*entities.PostDetailImage, error
	// - 原生 SQL (概念): SELECT * FROM post_detail_images WHERE post_id = ? AND deleted_at IS NULL ORDER BY display_order ASC
	// - 注意: 如果未找到图片，则返回空切片，而不是错误。
	GetImagesByPostDetailID(ctx context.Context, postDetailID uint64) ([]*entities.PostDetailImage, error)

	// BatchUpdateImages 批量更新图片信息，主要用于更新顺序 (DisplayOrder)。
	// - 意图: 高效地更新多张图片的 DisplayOrder 属性，例如在拖拽排序后统一更新。
	// - 输入: ctx context.Context, db *gorm.DB (用于事务操作), images []*entities.PostDetailImage
	// - 输出: error
	// - 注意: 每个 image 对象都应包含其 ID 和需要更新的 DisplayOrder。
	BatchUpdateImages(ctx context.Context, db *gorm.DB, images []*entities.PostDetailImage) error

	// DeleteImageByID 根据图片自身的ID删除帖子详情图片。
	// - 意图: 从数据库中移除指定的单张图片记录。
	// - 输入: ctx context.Context, db *gorm.DB (用于事务操作), imageID uint (假设BaseModel中的ID类型为uint)
	// - 输出: error
	// - 注意: 服务层在删除图片后，可能需要调整同postDetailID下其他图片的DisplayOrder。
	DeleteImageByID(ctx context.Context, db *gorm.DB, imageID uint) error

	// DeleteImagesByPostDetailID 删除与给定 postDetailID 关联的所有图片。
	// - 意图: 移除帖子详情的所有图片元数据，通常在更新帖子详情（图片整体替换）或删除帖子详情时使用。
	// - 输入: ctx context.Context, db *gorm.DB (用于事务操作), postDetailID uint
	// - 输出: error
	// - 原生 SQL (概念): DELETE FROM post_detail_images WHERE post_id = ?
	DeleteImagesByPostDetailID(ctx context.Context, db *gorm.DB, postDetailID uint64) error
}

type postDetailImageRepository struct {
	db *gorm.DB // GORM 数据库实例，用于非事务性的默认操作
}

// NewPostDetailImageRepository 创建 PostDetailImageRepository 的新实例。
func NewPostDetailImageRepository(db *gorm.DB) PostDetailImageRepository {
	return &postDetailImageRepository{db: db}
}

// CreateImage 创建单个帖子详情图片条目。
func (r *postDetailImageRepository) CreateImage(ctx context.Context, db *gorm.DB, image *entities.PostDetailImage) error {
	tx := db.WithContext(ctx) // 确保使用带有上下文的db实例
	if err := tx.Create(image).Error; err != nil {
		return err
	}
	return nil
}

// BatchCreatePostDetailImages 创建多个新的帖子详情图片条目。
func (r *postDetailImageRepository) BatchCreatePostDetailImages(ctx context.Context, db *gorm.DB, images []*entities.PostDetailImage) error {
	if len(images) == 0 {
		return nil // 没有要创建的内容
	}
	tx := db.WithContext(ctx)
	if err := tx.Create(&images).Error; err != nil {
		return err
	}
	return nil
}

// GetImageByID 根据图片ID获取图片详情。
func (r *postDetailImageRepository) GetImageByID(ctx context.Context, imageID uint) (*entities.PostDetailImage, error) {
	var image entities.PostDetailImage
	// 假设 entities.PostDetailImage 结构中嵌入了 gorm.Model 或 BaseModel (包含 ID)
	// 并且 ID 的类型是 uint
	if err := r.db.WithContext(ctx).First(&image, imageID).Error; err != nil {
		// GORM的First方法在未找到记录时会返回gorm.ErrRecordNotFound
		return nil, err
	}
	return &image, nil
}

// GetImagesByPostDetailID 检索与给定 postID 关联的所有图片。
func (r *postDetailImageRepository) GetImagesByPostDetailID(ctx context.Context, postDetailID uint64) ([]*entities.PostDetailImage, error) {
	var images []*entities.PostDetailImage
	// 使用仓库的默认 db 实例进行读取。
	// 根据 PostDetailImage 实体中的 DisplayOrder 字段进行排序。
	err := r.db.WithContext(ctx).Where("post_detail_id = ?", postDetailID).Order("display_order ASC").Find(&images).Error
	if err != nil {
		// GORM 的 Find 在未找到记录时不会返回 gorm.ErrRecordNotFound，而是返回一个空切片。
		return nil, err
	}
	return images, nil
}

// BatchUpdateImages 批量更新图片信息 (仅 DisplayOrder)。
func (r *postDetailImageRepository) BatchUpdateImages(ctx context.Context, db *gorm.DB, images []*entities.PostDetailImage) error {
	if len(images) == 0 {
		return nil
	}
	tx := db.WithContext(ctx)
	// GORM没有直接的批量更新不同记录为不同值的好方法，除非遍历或使用原生SQL。
	// 这里假设服务层会逐个调用UpdateImage，或者服务层构建了更复杂的批量更新逻辑。
	// 一个常见的做法是在事务中循环调用单条更新：
	for _, img := range images {
		// 确保每个img对象都有ID，并且待更新的 DisplayOrder 已设置
		// 使用 Save 会确保如果记录存在则更新，不存在则创建（需要ID预先设置）
		// 但这里更倾向于明确的 Update
		// 只更新 display_order 字段
		err := tx.Model(img).Select("display_order").Updates(map[string]interface{}{
			"display_order": img.DisplayOrder,
		}).Error
		// 或者更具体地指定模型和条件进行更新
		// err := tx.Model(&entities.PostDetailImage{}).Where("id = ?", img.ID).Select("display_order").Updates(map[string]interface{}{
		// "display_order": img.DisplayOrder,
		// }).Error

		if err != nil {
			return err // 如果任何一个更新失败，则返回错误，事务将回滚
		}
	}
	return nil
}

// DeleteImageByID 根据图片自身的ID删除帖子详情图片。
func (r *postDetailImageRepository) DeleteImageByID(ctx context.Context, db *gorm.DB, imageID uint) error {
	tx := db.WithContext(ctx)
	// 确保 entities.PostDetailImage 结构体中嵌入了 gorm.DeletedAt 以支持软删除
	// 如果没有 gorm.DeletedAt，这将是一个硬删除。
	result := tx.Delete(&entities.PostDetailImage{}, imageID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 如果希望在未找到要删除的记录时返回错误
		// return gorm.ErrRecordNotFound // 或者 commonerrors.ErrRepoNotFound
	}
	return nil
}

// DeleteImagesByPostDetailID 删除与给定 postDetailID 关联的所有图片。
func (r *postDetailImageRepository) DeleteImagesByPostDetailID(ctx context.Context, db *gorm.DB, postDetailID uint64) error {
	tx := db.WithContext(ctx)
	result := tx.Where("post_detail_id = ?", postDetailID).Delete(&entities.PostDetailImage{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}
