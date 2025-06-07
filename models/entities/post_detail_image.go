package entities

import "github.com/Xushengqwer/go-common/models/entities"

// PostDetailImage 帖子详情图片实体
//   - 使用场景: 存储帖子详情中每一张独立的图片信息。
//   - 表名: post_detail_images (GORM 默认会使用蛇形复数形式)
//   - 关系: 与 PostDetail 表为“多对一”关系 (一个 PostDetail 可以有多张 PostDetailImage)。
//     通过 PostDetailID 外键字段关联到 post_details 表的主键。
type PostDetailImage struct {
	entities.BaseModel // 嵌入自定义的 BaseModel, 通常包含 ID, CreatedAt, UpdatedAt, DeletedAt 字段

	// 关联的帖子详情ID (外键，指向 PostDetail 表的主键)
	// - GORM 标签:
	//   - not null: 确保每张图片都必须关联到一个帖子详情记录。
	//   - index: 为此外键添加数据库索引，以优化基于 PostDetailID 的查询性能（例如，获取某个帖子详情的所有图片）。
	PostDetailID uint64 `gorm:"not null;index"`

	// 图片URL或存储路径
	// - 类型: varchar(1023) 或 text。varchar(1023) 通常足够存储大部分URL，如果URL可能非常长，可以考虑使用 text。
	// - GORM 标签:
	//   - type:varchar(1023): 指定数据库中的字段类型。
	//   - not null: 图片的URL是必需的。
	ImageURL string `gorm:"type:varchar(1023);not null"`

	// 图片展示顺序 (可选字段)
	// - 类型: int。用于控制图片在前端展示时的顺序，例如 0, 1, 2...
	// - GORM 标签:
	//   - default:0: 如果不指定，默认为0。
	//   - index: 如果经常需要按顺序查询图片，可以考虑添加索引。
	DisplayOrder int `gorm:"default:0;index"`

	// 图片在COS中的ObjectKey
	ObjectKey string `gorm:"type:varchar(255);not null;index"`

	// 你还可以根据需要添加其他元数据字段，例如:
	// AltText string `gorm:"type:varchar(255)"` // 图片的SEO友好替代文本
	// MimeType string `gorm:"type:varchar(50)"`  // 图片的MIME类型, 如 "image/jpeg"
	// FileSize uint64                             // 图片文件大小 (单位：字节)
}
