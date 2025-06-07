package dto

import (
	"github.com/Xushengqwer/go-common/models/enums"
)

// ListPostsByConditionRequest 定义管理员分页条件查询帖子的请求数据结构
type ListPostsByConditionRequest struct {
	ID             *uint64            `form:"id" json:"id,omitempty"`                                            // 帖子ID，若存在则按主键查询，可选
	Title          *string            `form:"title" json:"title,omitempty"`                                      // 标题模糊查询，可选
	AuthorUsername *string            `form:"author_username" json:"author_username,omitempty"`                  // 作者用户名模糊查询，可选
	Status         *enums.Status      `form:"status" json:"status,omitempty" swaggertype:"integer"`              // 状态筛选，可选（0=待审核, 1=已审核, 2=拒绝）
	OfficialTag    *enums.OfficialTag `form:"official_tag" json:"official_tag,omitempty" swaggertype:"integer" ` // 官方标签筛选，可选
	ViewCountMin   *int64             `form:"view_count_min" json:"view_count_min,omitempty"`                    // 浏览量下限，可选
	ViewCountMax   *int64             `form:"view_count_max" json:"view_count_max,omitempty"`                    // 浏览量上限，可选
	OrderBy        string             `form:"order_by" json:"order_by"`                                          // 排序字段（created_at 或 updated_at），默认 created_at
	OrderDesc      bool               `form:"order_desc" json:"order_desc"`                                      // 是否降序，true 为降序
	Page           int                `form:"page" json:"page" binding:"required,gt=0"`                          // 页码，从 1 开始，必填
	PageSize       int                `form:"page_size" json:"page_size" binding:"required,gt=0"`                // 每页大小，必填
}

// AuditPostRequest 定义审核帖子的请求数据结构
type AuditPostRequest struct {
	PostID uint64 `json:"post_id" binding:"required" example:"123"` // 为 PostID 也添加一个 example
	// Status 表示帖子的审核状态。
	// 0: 待审核 (Pending)
	// 1: 审核通过 (Approved)
	// 2: 拒绝 (Rejected)
	Status enums.Status `json:"status" binding:"min=0,max=2" swaggertype:"integer" `
	Reason string       `json:"reason" binding:"omitempty,max=255" example:"内容符合规范"`
}

// UpdateOfficialTagRequest 定义更新帖子官方标签的请求数据结构
type UpdateOfficialTagRequest struct {
	PostID      uint64            `json:"post_id" binding:"required"`                                        // 帖子ID，必填
	OfficialTag enums.OfficialTag `json:"official_tag" swaggertype:"integer" binding:"required,min=0,max=3"` // 新的官方标签值，必填，并限制范围 (假设最大值为 3)
}
