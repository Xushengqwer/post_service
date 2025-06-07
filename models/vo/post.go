package vo

import (
	"github.com/Xushengqwer/go-common/models/enums"
	"github.com/Xushengqwer/post_service/models/entities"
	"time"
)

// PostResponse 定义了帖子基础信息的响应数据结构
type PostResponse struct {
	ID             uint64            `json:"id"`              // 帖子ID
	Title          string            `json:"title"`           // 帖子标题
	Status         enums.Status      `json:"status" `         // 帖子状态，0=待审核, 1=已审核, 2=拒绝
	ViewCount      int64             `json:"view_count"`      // 浏览量
	AuthorID       string            `json:"author_id"`       // 作者ID
	AuthorAvatar   string            `json:"author_avatar"`   // 作者头像
	AuthorUsername string            `json:"author_username"` // 作者用户名
	AuditReason    *string           `json:"audit_reason"`    // 审核原因 (如果 Status 为拒绝，则可能包含原因)
	OfficialTag    enums.OfficialTag `json:"official_tag" `   // 官方标签 (0=无, 1=官方认证, ...)
	CreatedAt      time.Time         `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time         `json:"updated_at"`      // 更新时间
}

// ListHotPostsByCursorResponse 查看热门帖子列表（基础信息）游标加载
type ListHotPostsByCursorResponse struct {
	Posts      []*PostResponse `json:"posts"`       // 帖子列表
	NextCursor *uint64         `json:"next_cursor"` // 下一个游标，nil 表示无更多数据
}

// PostTimelinePageVO 定义了帖子时间线分页查询的响应结构。
// - 包含当前页的帖子列表和下一页的游标信息。
type PostTimelinePageVO struct {
	Posts         []*PostResponse `json:"posts"`         // 当前页的帖子摘要列表
	NextCreatedAt *time.Time      `json:"nextCreatedAt"` // 下一页游标：创建时间，如果为nil表示没有下一页
	NextPostID    *uint64         `json:"nextPostId"`    // 下一页游标：帖子ID，如果为nil表示没有下一页
}

// ListUserPostPageVO 定义了自己的发帖的分页的查询响应结构。
// - 包含当前页的帖子列表和总记录数。
type ListUserPostPageVO struct {
	Posts []*PostResponse `json:"posts"` // 当前页的帖子列表
	Total int64           `json:"total"` // 符合条件的总记录数
}

// ListPostsAdminByConditionResponse 定义管理员按条件查询帖子基础信息的响应结构体
type ListPostsAdminByConditionResponse struct {
	Posts []*PostResponse `json:"posts"` // 帖子列表
	Total int64           `json:"total"` // 帖子总数
}

// MapPostsToPostResponsesVO 是一个辅助函数，用于将帖子实体列表转换为帖子响应VO列表。
// 如果这个函数需要在多个服务或包中使用，建议将其移至 vo 包下作为公共转换函数。
func MapPostsToPostResponsesVO(posts []*entities.Post) []*PostResponse {
	if len(posts) == 0 {
		return []*PostResponse{} // 返回空切片而不是nil，便于前端处理
	}

	responses := make([]*PostResponse, 0, len(posts))
	for _, post := range posts {
		if post == nil { // 安全检查，尽管不太可能发生
			continue
		}
		responses = append(responses, &PostResponse{
			ID:             post.ID,
			Title:          post.Title,
			Status:         post.Status,
			ViewCount:      post.ViewCount,
			AuthorID:       post.AuthorID,
			AuthorAvatar:   post.AuthorAvatar,
			AuthorUsername: post.AuthorUsername,
			OfficialTag:    post.OfficialTag,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
		})
	}
	return responses
}
