package dto

// CreatePostRequest 定义了创建帖子的请求数据结构
// - 添加了 binding 标签用于输入验证
type CreatePostRequest struct {
	Title          string  `json:"title" form:"title" binding:"required,max=100"`                    // 帖子标题，必填，最大100字符
	Content        string  `json:"content" form:"content" binding:"required,max=1000"`               // 帖子内容，必填，最大1000字符
	PricePerUnit   float64 `json:"price_per_unit" form:"price_per_unit" binding:"omitempty,gte=0"`   // 单价，可选，大于等于0
	ContactInfo    string  `json:"contact_info" form:"contact_info" binding:"omitempty"`             // 联系方式，可选
	AuthorID       string  `json:"author_id" form:"author_id" binding:"required"`                    // 作者ID，必填
	AuthorAvatar   string  `json:"author_avatar" form:"author_avatar" binding:"omitempty,url|uri"`   // 作者头像 URL，可选
	AuthorUsername string  `json:"author_username" form:"author_username" binding:"required,max=50"` // 作者用户名，必填，最大50字符

	// 注意：这里没有 Images 字段，因为图片文件是作为 multipart/form-data 的一部分直接上传的。
	// 如果需要前端传递图片顺序或其他元数据，可以考虑其他方式：
	// 1. 文件命名约定：后端根据文件名解析顺序。
	// 2. 额外的表单字段：例如 ImagesOrder []int `form:"images_order"` (需要前端确保与文件对应)。
	// 通常，如果文件是按顺序附加到 FormData 中的，后端按接收顺序处理是最简单的。
}

// ListPostsByUserIDRequest 定义分页查询用户帖子的请求数据结构（游标加载）
// - 添加了 form 和 binding 标签
type ListPostsByUserIDRequest struct {
	UserID   string  `json:"user_id" form:"user_id" binding:"required"`          // 用户ID，必填 (form tag 用于 query 参数绑定)
	Cursor   *uint64 `json:"cursor" form:"cursor"`                               // 游标（上次加载的最后一条帖子的 ID），可选
	PageSize int     `json:"page_size" form:"page_size" binding:"required,gt=0"` // 每页数量，必填，大于0
}
