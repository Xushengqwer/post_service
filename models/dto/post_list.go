package dto

import (
	"github.com/Xushengqwer/go-common/models/enums"
	"time"
)

// GetUserPostsRequestDTO 定义了用户获取自己帖子列表的API请求参数。
// - 用于控制器层接收和验证来自客户端的HTTP请求。
type GetUserPostsRequestDTO struct {
	// Page 页码，从 1 开始。
	// - 从URL查询参数 "page" 获取。
	// - binding:"required,gte=1"`: 必填，值必须大于等于1。
	Page int `form:"page" binding:"required,gte=1"`

	// PageSize 每页数量。
	// - 从URL查询参数 "pageSize" 获取。
	// - binding:"required,gte=1,lte=100"`: 必填，值必须在1到100之间。
	PageSize int `form:"pageSize" binding:"required,gte=1,lte=100"`

	// OfficialTag 官方标签筛选条件。
	// - 从URL查询参数 "officialTag" 获取。
	// - binding:"omitempty,min=0"`: 可选，如果提供，值必须大于等于0。
	OfficialTag *enums.OfficialTag `form:"officialTag" binding:"omitempty,min=0"`

	// Title 标题模糊搜索关键词。
	// - 从URL查询参数 "title" 获取。
	// - binding:"omitempty,max=255"`: 可选，如果提供，最大长度为255个字符。
	Title *string `form:"title" binding:"omitempty,max=255"`

	// Status 帖子审核状态筛选条件。
	// - 从URL查询参数 "status" 获取。
	// - binding:"omitempty,oneof=0 1 2"`: 可选，如果提供，必须是 0 (待审核), 1 (通过), 或 2 (拒绝) 之一。
	Status *enums.Status `form:"status" binding:"omitempty,oneof=0 1 2"`
}

// GetOffset 计算分页偏移量。
// - (page - 1) * pageSize
func (dto *GetUserPostsRequestDTO) GetOffset() int {
	if dto.Page <= 0 {
		return 0
	}
	return (dto.Page - 1) * dto.PageSize
}

// GetLimit 获取每页数量。
func (dto *GetUserPostsRequestDTO) GetLimit() int {
	return dto.PageSize
}

// GetPostsTimelineRequestDTO 定义了获取帖子时间线列表的API请求参数。
// - 用于控制器层接收和验证来自客户端的HTTP请求。
// - 标签如 `form` 用于从URL查询参数绑定，`binding` 用于参数验证。
type GetPostsTimelineRequestDTO struct {
	// LastCreatedAt 上一页最后一条记录的创建时间，用于游标分页。
	// - 从URL查询参数 "lastCreatedAt" 获取。
	// - binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`: 可选，如果提供，必须是 RFC3339 格式的时间字符串。
	LastCreatedAt *time.Time `form:"lastCreatedAt" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`

	// LastPostID 上一页最后一条记录的 ID，用于游标分页（辅助排序）。
	// - 从URL查询参数 "lastPostId" 获取。
	// - binding:"omitempty,gte=1"`: 可选，如果提供，必须大于等于1。
	LastPostID *uint64 `form:"lastPostId" binding:"omitempty,gte=1"`

	// PageSize 每页期望返回的记录数。
	// - 从URL查询参数 "pageSize" 获取。
	// - binding:"required,gte=1,lte=100"`: 必填，值必须在1到100之间。
	PageSize int `form:"pageSize" binding:"required,gte=1,lte=100"`

	// OfficialTag 官方标签筛选条件。
	// - 从URL查询参数 "officialTag" 获取。
	// - binding:"omitempty,min=0"`: 可选，如果提供，值必须大于等于0 (假设枚举的底层类型是int，且0是有效值如 "无标签")。
	//   请根据你的 enums.OfficialTag 的实际有效值范围调整 `min` 或使用 `oneof`。
	OfficialTag *enums.OfficialTag `form:"officialTag" binding:"omitempty,min=0"`

	// Title 标题模糊搜索关键词。
	// - 从URL查询参数 "title" 获取。
	// - binding:"omitempty,max=255"`: 可选，如果提供，最大长度为255个字符。
	Title *string `form:"title" binding:"omitempty,max=255"`

	// AuthorUsername 作者用户名模糊搜索关键词。
	// - 从URL查询参数 "authorUsername" 获取。
	// - binding:"omitempty,max=50"`: 可选，如果提供，最大长度为50个字符。
	AuthorUsername *string `form:"authorUsername" binding:"omitempty,max=50"`
}

// TimelineQueryDTO 封装了按时间线获取帖子列表的查询参数。
// - 用于在 Service 层和 Repo 层之间传递结构化的查询条件。
type TimelineQueryDTO struct {
	// LastCreatedAt 上一页最后一条记录的创建时间，用于游标分页。
	// - 类型为 *time.Time，允许为 nil，表示首次查询。
	LastCreatedAt *time.Time `json:"lastCreatedAt"`

	// LastPostID 上一页最后一条记录的 ID，用于游标分页（辅助排序）。
	// - 类型为 *uint64，允许为 nil，表示首次查询。
	LastPostID *uint64 `json:"lastPostID"`

	// PageSize 每页期望返回的记录数。
	PageSize int `json:"pageSize"`

	// OfficialTag 官方标签筛选条件。
	// - 类型为 *enums.OfficialTag，允许为 nil，表示不按官方标签筛选。
	OfficialTag *enums.OfficialTag `json:"officialTag"`

	// Title 标题模糊搜索关键词。
	// - 类型为 *string，允许为 nil，表示不按标题筛选。
	Title *string `json:"title"`

	// AuthorUsername 作者用户名模糊搜索关键词。
	// - 类型为 *string，允许为 nil，表示不按作者用户名筛选。
	AuthorUsername *string `json:"authorUsername"`
}
