package vo

// --- 用于成功响应且包含具体 Data 的包装器 ---

// ListPostsByUserIDResponseWrapper 对应 response.APIResponse[vo.ListPostsByUserIDResponse]
type ListPostsByUserIDResponseWrapper struct {
	Code    int                       `json:"code" example:"0"`
	Message string                    `json:"message,omitempty" example:"success"`
	Data    ListPostsByUserIDResponse `json:"data"` // 使用具体的 vo.ListPostsByUserIDResponse
}

// PostResponseWrapper 对应 response.APIResponse[vo.PostResponse]
type PostResponseWrapper struct {
	Code    int          `json:"code" example:"0"`
	Message string       `json:"message,omitempty" example:"success"`
	Data    PostResponse `json:"data"` // 使用具体的 vo.PostResponse
}

// PostDetailResponseWrapper 对应 response.APIResponse[vo.PostDetailResponse]
type PostDetailResponseWrapper struct {
	Code    int                `json:"code" example:"0"`
	Message string             `json:"message,omitempty" example:"success"`
	Data    PostDetailResponse `json:"data"` // 使用具体的 vo.PostDetailResponse
}

// ListPostsAdminResponseWrapper 对应 response.APIResponse[vo.ListPostsByConditionResponse]
type ListPostsAdminResponseWrapper struct {
	Code    int                          `json:"code" example:"0"`
	Message string                       `json:"message,omitempty" example:"success"`
	Data    ListPostsByConditionResponse `json:"data"` // 使用具体的 vo.ListPostsByConditionResponse
}

// --- 用于错误响应 或 简单成功响应（只有 Code 和 Message） ---

// BaseResponseWrapper 代表一个只包含 Code 和 Message 的响应。
// 适用于错误情况（RespondError 返回时 Data 为 nil 且 omitempty）
// 或某些成功操作（如 DELETE）可能也只返回 Code 和 Message。
type BaseResponseWrapper struct {
	Code    int    `json:"code" example:"0"`          // 成功时为 0, 错误时为具体错误码
	Message string `json:"message" example:"success"` // 成功或错误消息
	// 注意：这里没有 Data 字段，因为错误时它是 nil 且被 omitempty 省略了
}
