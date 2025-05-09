package dto

// 假设 enums 包已导入

// CreatePostRequest 定义了创建帖子的请求数据结构
// - 添加了 binding 标签用于输入验证
type CreatePostRequest struct {
	Title          string  `json:"title" binding:"required,max=100"`            // 帖子标题，必填，最大100字符
	Content        string  `json:"content" binding:"required,max=1000"`         // 帖子内容，必填，最大1000字符
	PricePerUnit   float64 `json:"price_per_unit" binding:"omitempty,gte=0"`    // 单价，可选，大于等于0 (omitempty 表示可选)
	ContactQRCode  string  `json:"contact_qr_code" binding:"omitempty,url|uri"` // 联系二维码 URL，可选，校验是否为 URL 或 URI (根据需要选择 url 或 uri)
	AuthorID       string  `json:"author_id" binding:"required"`                // 作者ID，必填 (假设是 UUID 或其他非空字符串)
	AuthorAvatar   string  `json:"author_avatar" binding:"omitempty,url|uri"`   // 作者头像 URL，可选
	AuthorUsername string  `json:"author_username" binding:"required,max=50"`   // 作者用户名，必填，最大50字符
}

// ListPostsByUserIDRequest 定义分页查询用户帖子的请求数据结构（游标加载）
// - 添加了 form 和 binding 标签
type ListPostsByUserIDRequest struct {
	UserID   string  `json:"user_id" form:"user_id" binding:"required"`          // 用户ID，必填 (form tag 用于 query 参数绑定)
	Cursor   *uint64 `json:"cursor" form:"cursor"`                               // 游标（上次加载的最后一条帖子的 ID），可选
	PageSize int     `json:"page_size" form:"page_size" binding:"required,gt=0"` // 每页数量，必填，大于0
}
