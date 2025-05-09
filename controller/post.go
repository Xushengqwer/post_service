package controller

import (
	"net/http"
	"strconv"

	"github.com/Xushengqwer/go-common/response" // 假设这是你的通用响应包
	"github.com/gin-gonic/gin"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/service"
)

// PostController 定义帖子控制器的结构体
type PostController struct {
	postService service.PostService // 服务层接口，通过依赖注入传入
}

// NewPostController 构造函数，用于创建 PostController 实例
func NewPostController(postService service.PostService) *PostController {
	return &PostController{
		postService: postService,
	}
}

// CreatePost 处理创建帖子的 HTTP 请求
// @Summary      创建新帖子
// @Description  使用提供的详情创建一个新帖子。请求体中需要包含作者信息。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        request body dto.CreatePostRequest true "创建帖子请求体"
// @Success      200 {object} vo.PostResponseWrapper "帖子创建成功" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求负载（例如，缺少字段，验证错误）" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "创建帖子时发生内部服务器错误" // <--- 修改
// @Router       /posts [post]
func (ctrl *PostController) CreatePost(c *gin.Context) {
	// 1. 从请求体中绑定 JSON 数据到 CreatePostRequest 结构体
	var req dto.CreatePostRequest
	// 假设 AuthorID, AuthorAvatar, AuthorUsername 在请求体中设置
	// 如果它们应该来自请求头/上下文，请调整 DTO 和绑定逻辑。
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的请求负载: "+err.Error()) // 包含绑定错误详情
		return
	}

	// 2. 调用服务层创建帖子
	// 传递请求上下文，如果中间件添加了用户信息，它可能包含这些信息
	post, err := ctrl.postService.CreatePost(c.Request.Context(), &req)
	if err != nil {
		// 如果可能，考虑根据错误类型进行更具体的错误处理
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "创建帖子失败: "+err.Error())
		return
	}

	// 3. 返回成功响应
	response.RespondSuccess(c, *post, "帖子创建成功")
}

// DeletePost 处理删除帖子的 HTTP 请求
// @Summary      删除帖子
// @Description  通过帖子的 ID 软删除一个帖子。需要在 URL 路径中提供帖子 ID。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        id path uint64 true "帖子 ID" Format(uint64)
// @Success      200 {object} vo.BaseResponseWrapper "帖子删除成功" // <--- 修改 (无 Data)
// @Failure      400 {object} vo.BaseResponseWrapper "无效的帖子 ID 格式" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "删除帖子时发生内部服务器错误" // <--- 修改
// @Router       /posts/{id} [delete]
func (ctrl *PostController) DeletePost(c *gin.Context) {
	// 1. 从 URL 参数获取帖子 ID
	idStr := c.Param("id")
	// 对 uint64 使用 ParseUint 并指定 bitSize 为 64
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的帖子 ID 格式")
		return
	}

	// 2. 调用服务层删除帖子
	if err := ctrl.postService.DeletePost(c.Request.Context(), id); err != nil {
		// 如果服务层返回特定错误（如'未找到'），可以考虑检查
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "删除帖子失败: "+err.Error())
		return
	}

	// 3. 返回成功响应
	response.RespondSuccess[any](c, nil, "帖子删除成功") // 运行时仍然可以传 nil data
}

// ListPostsByUserID 处理获取用户帖子列表的 HTTP 请求
// @Summary      根据用户 ID 列出帖子
// @Description  使用基于游标的分页方式，检索特定用户的分页帖子列表。需要在查询字符串中提供用户 ID 和分页参数。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        user_id query string true "用户 ID"
// @Param        cursor query uint64 false "游标（上一页最后一个帖子的 ID），首页省略" Format(uint64)
// @Param        page_size query int true "每页帖子数量" Format(int) minimum(1)
// @Success      200 {object} vo.ListPostsByUserIDResponseWrapper "帖子检索成功" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的输入参数（例如，缺少 user_id，无效的 page_size）" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "检索帖子时发生内部服务器错误" // <--- 修改
// @Router       /posts [get]
func (ctrl *PostController) ListPostsByUserID(c *gin.Context) {
	// 1. 构造请求结构体并绑定查询参数
	// 对于 GET 请求，使用 ShouldBindQuery 是合适的
	var req dto.ListPostsByUserIDRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的查询参数: "+err.Error())
		return
	}

	// 2. 额外的手动验证 (如果绑定标签不足以覆盖所有情况，例如必填字段)
	if req.UserID == "" {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "用户 ID 是必需的")
		return
	}
	// Gin 使用 gt=0 的绑定应该能处理 page_size > 0，但如果需要可以再次检查
	if req.PageSize <= 0 {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "页面大小必须大于 0")
		return
	}

	// 5. 调用服务层获取帖子列表
	result, err := ctrl.postService.ListPostsByUserID(c.Request.Context(), &req) // 传递绑定好的请求 DTO
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索帖子失败: "+err.Error())
		return
	}

	// 6. 返回成功响应
	response.RespondSuccess(c, *result, "帖子检索成功")
}

// GetPostDetailByPostID 处理获取帖子详情的 HTTP 请求
// @Summary      根据帖子 ID 获取帖子详情
// @Description  通过帖子的 ID 检索特定帖子的详细信息。需要在 URL 路径中提供帖子 ID，并从上下文（例如，通过中间件）获取 UserID。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        post_id path uint64 true "帖子 ID" Format(uint64)
// @Success      200 {object} vo.PostDetailResponseWrapper "帖子详情检索成功" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的帖子 ID 格式" // <--- 修改
// @Failure      401 {object} vo.BaseResponseWrapper "在上下文中未找到用户 ID（未授权）" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "检索帖子详情时发生内部服务器错误" // <--- 修改
// @Router       /posts/{post_id} [get]
func (ctrl *PostController) GetPostDetailByPostID(c *gin.Context) {
	// 1. 从 URL 参数获取帖子 ID
	postIDStr := c.Param("post_id")
	postID, err := strconv.ParseUint(postIDStr, 10, 64) // 使用 bitSize 64
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的帖子 ID 格式")
		return
	}

	// 4. 调用服务层获取帖子详情 ， 直接将请求的 Context (已经被中间件处理过) 传递给服务层
	detail, err := ctrl.postService.GetPostDetailByPostID(c.Request.Context(), postID)
	if err != nil {
		// 考虑检查特定错误，如 '未找到'
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索帖子详情失败: "+err.Error())
		return
	}

	// 5. 返回成功响应
	response.RespondSuccess(c, *detail, "帖子详情检索成功")
}

// RegisterRoutes 注册 PostController 的路由
func (ctrl *PostController) RegisterRoutes(group *gin.RouterGroup) {
	posts := group.Group("/posts") // 帖子的基础路径
	{                              // 使用花括号提高分组路由的可读性
		posts.POST("", ctrl.CreatePost)
		posts.DELETE("/:id", ctrl.DeletePost)              // 对路径参数统一使用 {id}
		posts.GET("", ctrl.ListPostsByUserID)              // 列出用户帖子（使用查询参数）
		posts.GET("/:post_id", ctrl.GetPostDetailByPostID) // 获取特定帖子详情
	}
}
