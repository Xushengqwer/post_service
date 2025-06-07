package vo

import (
	"github.com/Xushengqwer/post_service/models/entities"
	"time"

	"github.com/Xushengqwer/go-common/models/enums"
)

// PostDetailVO 定义了帖子详情页的完整视图对象。
// 它聚合了 Post 实体、PostDetail 实体以及 PostDetailImage 实体列表的信息。
type PostDetailVO struct {
	// --- 来自 Post 实体 ---
	ID             uint64            `json:"id"`              // 帖子ID
	CreatedAt      time.Time         `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time         `json:"updated_at"`      // 更新时间
	Title          string            `json:"title"`           // 帖子标题
	AuthorID       string            `json:"author_id"`       // 作者ID
	AuthorAvatar   string            `json:"author_avatar"`   // 作者头像URL
	AuthorUsername string            `json:"author_username"` // 作者用户名
	ViewCount      int64             `json:"view_count"`      // 浏览量
	OfficialTag    enums.OfficialTag `json:"official_tag"`    // 官方标签 (参考 enums.OfficialTag)

	// --- 来自 PostDetail 实体 ---
	Content      string  `json:"content"`        // 帖子详细HTML内容
	PricePerUnit float64 `json:"price_per_unit"` // 单价 (单位：元)
	ContactInfo  string  `json:"contact_info"`   // 联系方式 (手机号、微信号、QQ号等)

	// --- 来自 PostDetailImage 实体列表 ---
	// Images 字段存储了帖子的所有详情图片，并已按 DisplayOrder 排序。
	Images []PostImageVO `json:"images"` // 详情图片列表
}

// PostImageVO 定义了帖子详情中单张图片的视图对象。
// 用于在 PostDetailVO 中表示图片列表。
type PostImageVO struct {
	ImageURL     string `json:"image_url"`     // 图片URL
	DisplayOrder int    `json:"display_order"` // 图片展示顺序
	ObjectKey    string `json:"object_key"`    // 图片在COS中的ObjectKey
}

// NewPostImageVOFromEntity 将单个 PostDetailImage 实体转换为 PostImageVO。
// 此函数会处理输入实体可能为 nil 的情况。
func NewPostImageVOFromEntity(entity *entities.PostDetailImage) PostImageVO { // 请确保 entities.PostDetailImage 类型是您项目中正确的类型
	if entity == nil {
		// 根据期望的行为，返回一个零值的 PostImageVO 或作为错误处理。
		// 对于 VO 来说，返回零值通常是可以接受的。
		return PostImageVO{}
	}
	return PostImageVO{
		ImageURL:     entity.ImageURL,
		DisplayOrder: entity.DisplayOrder,
		ObjectKey:    entity.ObjectKey,
	}
}

// NewPostImageVOsFromEntities 将 PostDetailImage 实体切片转换为 PostImageVO 切片。
// 此函数会处理 nil 或空切片，以及切片中可能存在的 nil 元素。
func NewPostImageVOsFromEntities(entities []*entities.PostDetailImage) []PostImageVO { // 请确保 entities.PostDetailImage 类型是您项目中正确的类型
	if len(entities) == 0 {
		return make([]PostImageVO, 0) // 返回一个空的、非 nil 的切片，以便JSON序列化为 [] 而不是 null
	}

	vos := make([]PostImageVO, 0, len(entities)) // 预分配切片容量以提高效率
	for _, entity := range entities {
		if entity != nil { // 安全地跳过切片中可能存在的 nil 实体
			vos = append(vos, NewPostImageVOFromEntity(entity))
		}
	}
	return vos
}
