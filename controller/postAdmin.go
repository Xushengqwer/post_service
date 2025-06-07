package controller

import (
	"errors"
	"github.com/Xushengqwer/go-common/constants"
	"net/http"
	"strconv" // 如果需要在路径中添加 ID 参数，则需要此包

	"github.com/Xushengqwer/go-common/commonerrors" // 假设包含 ErrRepoNotFound 等通用错误
	"github.com/Xushengqwer/go-common/response"     // 假设这是你的通用响应包
	"github.com/gin-gonic/gin"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/service"
)

// PostAdminController 定义帖子管理员控制器的结构体
type PostAdminController struct {
	adminService service.PostAdminService // 服务层接口
}

// NewPostAdminController 构造函数，注入服务层依赖
func NewPostAdminController(adminService service.PostAdminService) *PostAdminController {
	return &PostAdminController{
		adminService: adminService,
	}
}

// AuditPost 处理管理员审核帖子的 HTTP 请求
// @Summary      审核帖子
// @Description  管理员更新帖子的状态（以及可选的原因）。需要在请求体中提供审核详情。
// @Tags         admin-posts (管理员-帖子)
// @Accept       json
// @Produce      json
// @Param        request body dto.AuditPostRequest true "审核帖子请求体"
// @Success      200 {object} vo.BaseResponseWrapper "帖子审核成功" // <--- 修改 (无 Data)
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求负载（例如，缺少字段，无效的状态）" // <--- 修改
// @Failure      404 {object} vo.BaseResponseWrapper "帖子未找到" // <-- 添加404情况
// @Failure      500 {object} vo.BaseResponseWrapper "审核过程中发生内部服务器错误" // <--- 修改
// @Router       /api/v1/post/admin/posts/audit [post]
func (ctrl *PostAdminController) AuditPost(c *gin.Context) {
	// 1. 从请求体绑定 JSON 数据
	var req dto.AuditPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的请求负载: "+err.Error())
		return
	}

	// 如果绑定不能覆盖 Status 枚举的验证，可以在这里添加潜在的验证
	// 例如：if req.Status < enums.Pending || req.Status > enums.Rejected { ... }

	// 2. 调用服务层审核帖子
	// 假设 AuditPost 能恰当处理未找到的错误
	if err := ctrl.adminService.AuditPost(c.Request.Context(), &req); err != nil {
		// 处理服务层可能返回的 '未找到' 错误
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			response.RespondError(c, http.StatusNotFound, response.ErrCodeClientResourceNotFound, "审核的帖子未找到")
		} else {
			response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "审核帖子失败: "+err.Error())
		}
		return
	}

	// 3. 返回成功响应
	response.RespondSuccess[any](c, nil, "帖子审核成功") // 运行时仍然可以传 nil data
}

// ListPostsByCondition 处理按条件查询帖子列表的 HTTP 请求
// @Summary      按条件列出帖子 (管理员)
// @Description  出于管理目的，根据各种过滤条件检索分页的帖子列表。使用查询参数进行过滤和分页。
// @Tags         admin-posts (管理员-帖子)
// @Accept       json
// @Produce      json
// @Param        id query uint64 false "按精确的帖子 ID 过滤" Format(uint64)
// @Param        title query string false "按帖子标题过滤（模糊匹配）"
// @Param        author_username query string false "按作者用户名过滤（模糊匹配）"
// @Param        status query int false "按帖子状态过滤 (0=待审核, 1=已审核, 2=已拒绝)" Enums(0, 1, 2)
// @Param        official_tag query int false "按官方标签过滤 (例如, 0=无, 1=官方认证)" Enums(0, 1, 2, 3)
// @Param        view_count_min query int64 false "按最小浏览量过滤" Format(int64)
// @Param        view_count_max query int64 false "按最大浏览量过滤" Format(int64)
// @Param        order_by query string false "排序字段 (created_at 或 updated_at)" Enums(created_at, updated_at) default(created_at)
// @Param        order_desc query bool false "是否降序排序 (true 为 DESC, false/省略为 ASC)" default(false)
// @Param        page query int true "页码（从 1 开始）" Format(int) minimum(1)
// @Param        page_size query int true "每页帖子数量" Format(int) minimum(1)
// @Success      200 {object} vo.ListPostsAdminResponseWrapper "帖子检索成功" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的输入参数（例如，无效的 page, page_size, status）" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "检索帖子时发生内部服务器错误" // <--- 修改
// @Router       /api/v1/post/admin/posts [get]
func (ctrl *PostAdminController) ListPostsByCondition(c *gin.Context) {
	// 1. 绑定查询参数到 DTO
	var req dto.ListPostsByConditionRequest
	// ShouldBindQuery 适用于 GET 请求的查询参数
	if err := c.ShouldBindQuery(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的查询参数: "+err.Error())
		return
	}

	// 如果绑定标签不足，可以在此添加手动验证（例如，如果绑定未处理枚举范围）
	if req.Page <= 0 {
		req.Page = 1 // 如果无效或缺失，默认为第 1 页
	}
	if req.PageSize <= 0 {
		req.PageSize = 10 // 如果无效或缺失，默认页面大小为 10
	}
	// 如果需要，验证 OrderBy
	if req.OrderBy != "created_at" && req.OrderBy != "updated_at" {
		req.OrderBy = "created_at" // 默认排序字段
	}

	// 2. 调用服务层查询帖子列表
	result, err := ctrl.adminService.ListPostsByCondition(c.Request.Context(), &req)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索帖子失败: "+err.Error())
		return
	}

	// 3. 返回成功响应
	// 在解引用之前确保 result 不是 nil，尽管服务理想情况下应该在没有错误时返回空结构体
	if result == nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "服务在没有错误的情况下返回了 nil 结果")
		return
	}
	response.RespondSuccess(c, *result, "帖子检索成功")
}

// UpdateOfficialTag 处理管理员更新帖子官方标签的 HTTP 请求
// @Summary      更新帖子官方标签 (管理员)
// @Description  管理员更新特定帖子的官方标签。需要在 URL 路径中提供帖子 ID，并在请求体中提供标签详情。
// @Tags         admin-posts (管理员-帖子)
// @Accept       json
// @Produce      json
// @Param        id path uint64 true "要更新的帖子 ID" Format(uint64)
// @Param        request body dto.UpdateOfficialTagRequest true "更新官方标签请求体 (请求体中的 PostID 是冗余的，请使用路径中的 ID)"
// @Success      200 {object} vo.BaseResponseWrapper "官方标签更新成功" // <--- 修改 (无 Data)
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求负载，无效的标签值，或路径 ID 与请求体 ID 不匹配" // <--- 修改
// @Failure      404 {object} vo.BaseResponseWrapper "帖子未找到" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "更新标签时发生内部服务器错误" // <--- 修改
// @Router       /api/v1/post/admin/posts/{id}/official-tag [put] // 改为 PUT，因为是更新操作
func (ctrl *PostAdminController) UpdateOfficialTag(c *gin.Context) {
	// 1. 从 URL 路径参数获取帖子 ID
	idStr := c.Param("id")
	pathPostID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "URL 路径中的帖子 ID 格式无效")
		return
	}

	// 2. 从请求体绑定 JSON 数据
	var req dto.UpdateOfficialTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的请求负载: "+err.Error())
		return
	}

	// 3. 可选：验证请求体中的 PostID 是否与路径中的 PostID 匹配（如果请求体包含 PostID）
	if req.PostID != 0 && req.PostID != pathPostID {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "请求体中的帖子 ID 与 URL 路径不匹配")
		return
	}
	// 使用路径中的 ID 进行服务调用
	req.PostID = pathPostID

	// 4. 调用服务层更新官方标签
	if err := ctrl.adminService.UpdateOfficialTag(c.Request.Context(), &req); err != nil {
		// 根据服务层返回的错误类型判断是 404 还是 500
		if errors.Is(err, commonerrors.ErrRepoNotFound) { // 假设服务层返回或包装了此错误
			response.RespondError(c, http.StatusNotFound, response.ErrCodeClientResourceNotFound, "帖子未找到")
		} else {
			response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "更新官方标签失败: "+err.Error())
		}
		return
	}

	// 5. 返回成功响应
	response.RespondSuccess[any](c, nil, "官方标签更新成功") // 运行时仍然可以传 nil data
}

// DeletePostByAdmin 处理管理员删除帖子的请求
// @Summary 管理员删除帖子 (Admin delete post)
// @Description 管理员软删除指定ID的帖子 (Admin soft deletes a post with the specified ID)
// @Tags Admin
// @Accept json
// @Produce json
// @Param post_id path string true "帖子ID (Post ID)"
// @Success 200 {object} vo.BaseResponseWrapper "帖子删除成功"
// @Failure 400 {object} vo.BaseResponseWrapper "无效的帖子ID格式"
// @Failure 401 {object} vo.BaseResponseWrapper "管理员未登录或无权限"
// @Failure 404 {object} vo.BaseResponseWrapper "帖子未找到"
// @Failure 500 {object} vo.BaseResponseWrapper "删除帖子时发生内部服务器错误"
// @Router /api/v1/post/admin/posts/{post_id} [delete]
func (s *PostAdminController) DeletePostByAdmin(c *gin.Context) {
	// 1. 从 URL 路径参数获取帖子 ID
	postIDStr := c.Param("post_id")
	if postIDStr == "" {
		// 假设 response 包和 ErrCodeClientMissingParam 存在
		// 如果不存在，可以替换为 response.ErrCodeClientInvalidInput 或其他合适的错误码
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "URL 路径中缺少帖子 ID")
		return
	}

	postID, err := strconv.ParseUint(postIDStr, 10, 64)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "URL 路径中的帖子 ID 格式无效")
		return
	}

	// 2. 从 Gin 上下文中获取管理员用户 ID
	adminIDInterface, exists := c.Get(string(constants.UserIDKey)) // 假设 constants.UserIDKey 已定义
	if !exists {
		response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "无法获取管理员ID，用户可能未登录或凭证缺失")
		return
	}

	adminID, ok := adminIDInterface.(string)
	if !ok || adminID == "" { // 确保 adminID 是字符串且不为空
		response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "管理员ID格式无效或为空")
		return
	}

	// 3. 调用服务层方法删除帖子
	// 确保 s.adminService 字段在 PostAdminController 中已正确初始化
	// DeletePostByAdmin(ctx context.Context, postID uint64, adminUserID string) error
	err = s.adminService.DeletePostByAdmin(c.Request.Context(), postID, adminID)
	if err != nil {
		if errors.Is(err, commonerrors.ErrRepoNotFound) { // 假设 myErrors.ErrPostNotFound 存在
			response.RespondError(c, http.StatusNotFound, response.ErrCodeClientResourceNotFound, "帖子未找到")
		} else {
			// 对于其他来自服务层的错误，统一处理为内部服务器错误
			response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "删除帖子失败: "+err.Error())
		}
		return
	}

	// 4. 返回成功响应
	response.RespondSuccess[any](c, nil, "帖子删除成功")
}

// RegisterRoutes 注册 PostAdminController 的路由
func (ctrl *PostAdminController) RegisterRoutes(group *gin.RouterGroup) {
	adminPosts := group.Group("/admin/posts") // 基础路径 /admin/posts
	{
		adminPosts.POST("/audit", ctrl.AuditPost)                   // POST /admin/posts/audit
		adminPosts.GET("", ctrl.ListPostsByCondition)               // GET /admin/posts
		adminPosts.PUT("/:id/official-tag", ctrl.UpdateOfficialTag) // PUT /admin/posts/{id}/official-tag
		adminPosts.DELETE("/:post_id", ctrl.DeletePostByAdmin)
	}
}
