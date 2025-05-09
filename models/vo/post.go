package vo

import "time"

//  todo 这里应该是有问题的，应该是共用一个响应

// PostResponse 定义了创建帖子后返回的数据结构
type PostResponse struct {
	ID             uint64    `json:"id"`              // 帖子ID
	Title          string    `json:"title"`           // 帖子标题
	Status         uint      `json:"status"`          // 帖子状态，0=待审核, 1=已审核, 2=拒绝
	ViewCount      int64     `json:"view_count"`      // 浏览量
	AuthorID       string    `json:"author_id"`       // 作者ID
	AuthorAvatar   string    `json:"author_avatar"`   // 作者头像
	AuthorUsername string    `json:"author_username"` // 作者用户名
	CreatedAt      time.Time `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time `json:"updated_at"`      // 更新时间
}

type ListPostsByUserIDResponse struct {
	Posts      []*PostResponse `json:"posts"`       // 帖子列表
	NextCursor *uint64         `json:"next_cursor"` // 下一个游标，nil 表示无更多数据
}

// PostDetailResponse 定义帖子详情返回的数据结构
type PostDetailResponse struct {
	ID             uint64    `json:"id"`              // 帖子详情ID
	PostID         uint64    `json:"post_id"`         // 帖子ID
	Content        string    `json:"content"`         // 帖子内容
	PricePerUnit   float64   `json:"price_per_unit"`  // 单价
	ContactQRCode  string    `json:"contact_qr_code"` // 联系二维码
	AuthorID       string    `json:"author_id"`       // 作者ID
	AuthorAvatar   string    `json:"author_avatar"`   // 作者头像
	AuthorUsername string    `json:"author_username"` // 作者用户名
	CreatedAt      time.Time `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time `json:"updated_at"`      // 更新时间
}
