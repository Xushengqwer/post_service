package vo

import (
	"github.com/Xushengqwer/go-common/models/enums"
	"time"
)

// PostAdminResponse 定义管理员查询帖子列表的返回数据结构
type PostAdminResponse struct {
	ID             uint64            `json:"id"`              // 帖子ID
	Title          string            `json:"title"`           // 帖子标题
	AuthorID       string            `json:"author_id"`       // 作者ID
	AuthorUsername string            `json:"author_username"` // 作者用户名
	AuthorAvatar   string            `json:"author_avatar"`   // 作者头像
	Status         enums.Status      `json:"status"`          // 帖子状态（0=待审核, 1=已审核, 2=拒绝）
	ViewCount      int64             `json:"view_count"`      // 浏览量
	OfficialTag    enums.OfficialTag `json:"official_tag"`    // 官方标签
	CreatedAt      time.Time         `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time         `json:"updated_at"`      // 更新时间
}

// ListPostsByConditionResponse 定义按条件查询帖子的响应结构体
type ListPostsByConditionResponse struct {
	Posts []*PostAdminResponse `json:"posts"` // 帖子列表
	Total int64                `json:"total"` // 帖子总数
}
