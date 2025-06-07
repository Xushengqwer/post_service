package controller

import (
	"github.com/Xushengqwer/go-common/constants"
	"net/http"
	"strconv"

	"github.com/Xushengqwer/go-common/response" // 你的通用响应包
	"github.com/gin-gonic/gin"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/service"
)

// PostController 定义帖子控制器的结构体
type PostController struct {
	postService     service.PostService // 服务层接口，通过依赖注入传入
	PostListService service.PostListService
}

// NewPostController 构造函数，用于创建 PostController 实例
func NewPostController(postService service.PostService, PostListService service.PostListService) *PostController {
	return &PostController{
		postService:     postService,
		PostListService: PostListService,
	}
}

// GetUserPosts 获取当前用户自己的帖子列表 (分页)
// @Summary      获取我的帖子列表
// @Description  获取当前登录用户发布的帖子列表，支持按官方标签、标题、帖子状态筛选，并使用分页加载。UserID 从请求上下文中获取。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        page query int true "页码 (从1开始)" format(int32) minimum(1) default(1)
// @Param        pageSize query int true "每页数量" format(int32) minimum(1) maximum(100) default(10)
// @Param        officialTag query int false "官方标签 (0:无标签, 1:官方认证, 2:预付保证金, 3:急速响应)" format(int32) Enums(0,1,2,3)
// @Param        title query string false "标题模糊搜索关键词 (最大长度 255)" maxLength(255)
// @Param        status query int false "帖子状态 (0:待审核, 1:审核通过, 2:拒绝)" format(int32) Enums(0,1,2)
// @Success      200 {object} vo.ListUserPostPageResponseWrapper "成功响应，包含用户帖子列表和总记录数"
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求参数"
// @Failure      401 {object} vo.BaseResponseWrapper "用户未授权或认证失败"
// @Failure      500 {object} vo.BaseResponseWrapper "服务器内部错误"
// @Router       /api/v1/post/posts/mine [get]
func (ctrl *PostController) GetUserPosts(c *gin.Context) {
	// 1. 绑定并验证查询参数
	var reqDTO dto.GetUserPostsRequestDTO
	if err := c.ShouldBindQuery(&reqDTO); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的查询参数: "+err.Error())
		return
	}

	//  从gin.context中取出来从网关服务透传下来的userID
	userIDValue, exists := c.Get(string(constants.UserIDKey)) // 使用 c.Get()
	if !exists {
		// 如果中间件设置了值，这里不应该发生。但以防万一，还是检查一下。
		response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "无法获取用户信息 (Context Key Not Found)")
		return
	}

	userID, ok := userIDValue.(string)
	if !ok || userID == "" {
		// 类型断言失败或 UserID 为空
		response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "无法获取有效的用户 ID (Invalid UserID in Context)")
		return
	}

	// 2. 调用服务层方法
	// UserID 将在服务层从 c.Request.Context() 中获取
	ListUserPostPageVO, err := ctrl.PostListService.GetUserPosts(c.Request.Context(), userID, &reqDTO) // <--- 修改了这里
	if err != nil {
		if err.Error() == "unauthorized" { // 简单示例，实际应使用 errors.Is 和 commonerrors.ErrUnauthorized
			response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "用户未授权: "+err.Error())
		} else {
			response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "获取用户帖子列表失败: "+err.Error())
		}
		return
	}

	// 3. 成功响应
	response.RespondSuccess(c, ListUserPostPageVO, "用户帖子列表获取成功")
}

// GetPostsTimeline 获取帖子时间线列表 (游标分页)
// @Summary      获取帖子时间线列表 (公开)
// @Description  根据指定条件（官方标签、标题、作者用户名）和游标分页获取帖子列表，按时间倒序排列。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        lastCreatedAt query string false "上一页最后一条记录的创建时间 (RFC3339格式, e.g., 2023-01-01T15:04:05Z)" format(date-time)
// @Param        lastPostId query uint64 false "上一页最后一条记录的帖子ID" format(uint64) minimum(1)
// @Param        pageSize query int true "每页数量" format(int32) minimum(1) maximum(100) default(10)
// @Param        officialTag query int false "官方标签 (0:无标签, 1:官方认证, 2:预付保证金, 3:急速响应)" format(int32) Enums(0,1,2,3)
// @Param        title query string false "标题模糊搜索关键词 (最大长度 255)" maxLength(255)
// @Param        authorUsername query string false "作者用户名模糊搜索关键词 (最大长度 50)" maxLength(50)
// @Success      200 {object} vo.PostTimelinePageResponseWrapper "成功响应，包含帖子列表和下一页游标信息"
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求参数"
// @Failure      500 {object} vo.BaseResponseWrapper "服务器内部错误"
// @Router       /api/v1/post/posts/timeline [get]
func (ctrl *PostController) GetPostsTimeline(c *gin.Context) {
	var reqDTO dto.GetPostsTimelineRequestDTO
	if err := c.ShouldBindQuery(&reqDTO); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的查询参数: "+err.Error())
		return
	}
	serviceQueryDTO := &dto.TimelineQueryDTO{
		LastCreatedAt:  reqDTO.LastCreatedAt,
		LastPostID:     reqDTO.LastPostID,
		PageSize:       reqDTO.PageSize,
		OfficialTag:    reqDTO.OfficialTag,
		Title:          reqDTO.Title,
		AuthorUsername: reqDTO.AuthorUsername,
	}
	timelinePageVO, err := ctrl.PostListService.GetPostsByTimeline(c.Request.Context(), serviceQueryDTO)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "获取帖子列表失败: "+err.Error())
		return
	}
	response.RespondSuccess(c, timelinePageVO, "帖子时间线获取成功")
}

// CreatePost 处理创建帖子的 HTTP 请求，包含图片上传。
// DTO 字段作为独立的表单字段提交。
// @Summary      创建新帖子 (独立表单字段及图片)
// @Description  使用提供的详情（作为独立表单字段）和图片文件创建一个新帖子。请求体应为 multipart/form-data。
// @Tags         posts (帖子)
// @Accept       multipart/form-data
// @Produce      json
// @Param        title formData string true "帖子标题" maxLength(100)
// @Param        content formData string true "帖子内容" maxLength(1000)
// @Param        price_per_unit formData number false "单价 (可选, 大于等于0)" minimum(0)
// @Param        contact_info formData string false "联系方式 (可选)"
// @Param        author_id formData string true "作者ID"
// @Param        author_avatar formData string false "作者头像 URL (可选, 需为有效URL)" format(url)
// @Param        author_username formData string true "作者用户名" maxLength(50)
// @Param        images formData file true "帖子图片文件 (可多选)"
// @Success      200 {object} vo.PostDetailResponseWrapper "帖子创建成功"
// @Failure      400 {object} vo.BaseResponseWrapper "无效的请求负载或文件处理错误"
// @Failure      500 {object} vo.BaseResponseWrapper "创建帖子时发生内部服务器错误"
// @Router       /api/v1/post/posts [post]
func (ctrl *PostController) CreatePost(c *gin.Context) {
	// 1. 解析 Multipart Form (确保在访问表单数据或文件之前调用)
	// 设置表单解析的最大内存，超出部分会存到临时磁盘文件
	// 例如：32MB (32 << 20)
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "解析表单数据失败: "+err.Error())
		return
	}

	// 2. 绑定 DTO 数据 (来自独立的表单字段)
	var req dto.CreatePostRequest
	// ShouldBind 会尝试根据 Content-Type 进行绑定。
	// 对于 multipart/form-data，它会尝试从表单值填充结构体。
	// DTO 中的 `form` 标签（如果提供）或 `json` 标签（作为回退）或字段名会用于匹配。
	// `binding` 标签仍然会用于验证。
	if err := c.ShouldBind(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "绑定请求数据失败: "+err.Error())
		return
	}

	// 3. 获取图片文件部分
	// "images" 是前端 FormData.append("images", file) 时使用的字段名
	// c.Request.MultipartForm 可以在 ParseMultipartForm 之后安全调用
	form := c.Request.MultipartForm // 获取已解析的表单
	if form == nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "未能获取 multipart form 数据")
		return
	}
	imageFiles := form.File["images"] // "images" 是前端上传文件时使用的字段名

	// 可选：校验图片文件数量等
	if len(imageFiles) == 0 {
		// 根据业务需求，如果图片非必须，可以移除此日志或判断
		// ctrl.logger.Info("没有上传图片文件，将继续创建不含图片的帖子", zap.String("author_id", req.AuthorID))
	}

	// 4. 调用服务层处理
	postDetailVO, serviceErr := ctrl.postService.CreatePost(c.Request.Context(), &req, imageFiles)
	if serviceErr != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "创建帖子失败: "+serviceErr.Error())
		return
	}

	response.RespondSuccess(c, postDetailVO, "帖子创建成功")
}

// DeletePost 处理普通用户删除帖子的 HTTP 请求
// @Summary      删除指定ID的帖子
// @Description  通过帖子的 ID 软删除一个帖子。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        id path uint64 true "帖子 ID" Format(uint64)
// @Success      200 {object} vo.BaseResponseWrapper "帖子删除成功"
// @Failure      400 {object} vo.BaseResponseWrapper "无效的帖子 ID 格式"
// @Failure      500 {object} vo.BaseResponseWrapper "删除帖子时发生内部服务器错误"
// @Router       /api/v1/post/posts/{id} [delete]
func (ctrl *PostController) DeletePost(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的帖子 ID 格式")
		return
	}
	if err := ctrl.postService.DeletePost(c.Request.Context(), id); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "删除帖子失败: "+err.Error())
		return
	}
	response.RespondSuccess[any](c, nil, "帖子删除成功")
}

// ListPostsByUserID 处理获取指定用户公开发布的帖子列表 (游标加载)
// @Summary      获取指定用户的帖子列表 (公开, 游标加载)
// @Description  使用游标分页方式，检索特定用户公开发布的帖子列表。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        user_id query string true "要查询其帖子的用户 ID"
// @Param        cursor query uint64 false "游标（上一页最后一个帖子的 ID），首页省略" Format(uint64)
// @Param        page_size query int true "每页帖子数量" Format(int) minimum(1)
// @Success      200 {object} vo.ListPostsByCursorResponseWrapper "帖子检索成功" // 确保 vo.ListPostsByUserIDResponseWrapper 对应游标加载的响应结构
// @Failure      400 {object} vo.BaseResponseWrapper "无效的输入参数"
// @Failure      500 {object} vo.BaseResponseWrapper "检索帖子时发生内部服务器错误"
// @Router       /api/v1/post/posts/by-author [get]  // <--- 路径已修改
func (ctrl *PostController) ListPostsByUserID(c *gin.Context) {
	// 1. 构造请求结构体并绑定查询参数
	// 对于 GET 请求，使用 ShouldBindQuery 是合适的
	var req dto.ListPostsByUserIDRequest // 这个 DTO 应该是你为游标加载设计的
	if err := c.ShouldBindQuery(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的查询参数: "+err.Error())
		return
	}

	// 2. 额外的手动验证 (如果绑定标签不足以覆盖所有情况)
	//    你的 dto.ListPostsByUserIDRequest 应该已经通过 binding:"required" 验证了 UserID 和 PageSize
	if req.UserID == "" { // 再次确认，以防万一或 binding 标签有误
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "用户 ID 是必需的")
		return
	}
	if req.PageSize <= 0 { // 再次确认
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "页面大小必须大于 0")
		return
	}

	// 5. 调用服务层获取帖子列表
	result, err := ctrl.PostListService.ListPostsByUserID(c.Request.Context(), &req) // 传递绑定好的请求 DTO
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索帖子失败: "+err.Error())
		return
	}

	// 6. 返回成功响应
	// 假设你的 ctrl.postService.ListPostsByUserID 返回的是 *vo.ListPostsByUserIDResponse (或类似的游标响应结构)
	// 并且你的 response.RespondSuccess 能够正确处理它。
	// 如果 ListPostsByUserID 返回的是指针，而 RespondSuccess 期望值，你可能需要解引用 *result。
	// 但根据你之前的 CreatePost 和 GetPostDetailByPostID，你传递的是 *post 和 *detail，所以这里保持一致。
	response.RespondSuccess(c, result, "帖子检索成功")
}

// GetPostDetailByPostID 处理获取帖子详情的 HTTP 请求
// @Summary      获取指定ID的帖子详情 (公开)
// @Description  通过帖子的 ID 检索特定帖子的详细信息。同时，如果用户已登录（通过中间件注入UserID），则会尝试增加浏览量。
// @Tags         posts (帖子)
// @Accept       json
// @Produce      json
// @Param        post_id path uint64 true "帖子 ID" Format(uint64)
// @Param        X-User-ID header string false "用户 ID (由网关/中间件注入)"
// @Success      200 {object} vo.PostDetailResponseWrapper "帖子详情检索成功"
// @Failure      400 {object} vo.BaseResponseWrapper "无效的帖子 ID 格式"
// @Failure      500 {object} vo.BaseResponseWrapper "检索帖子详情时发生内部服务器错误"
// @Router       /api/v1/post/posts/{post_id} [get]
func (ctrl *PostController) GetPostDetailByPostID(c *gin.Context) {
	postIDStr := c.Param("post_id")
	postID, err := strconv.ParseUint(postIDStr, 10, 64)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的帖子 ID 格式")
		return
	}

	// 从 gin.Context 中获取 UserID (由 UserContextMiddleware 注入)
	// 如果获取不到（例如未登录用户），userID 会是空字符串""
	userID := c.GetString(string(constants.UserIDKey)) // 使用 GetString 更安全，如果 key 不存在会返回 ""

	// 将 gin.Context 中的 Request.Context() 和获取到的 UserID 传递给服务层
	detail, err := ctrl.postService.GetPostDetailByPostID(c.Request.Context(), postID, userID)
	if err != nil {
		// 这里可以根据 service 返回的错误类型，决定返回 404 还是 500
		// 暂时保持 500，但可以细化
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索帖子详情失败: "+err.Error())
		return
	}

	response.RespondSuccess(c, detail, "帖子详情检索成功")
}

// RegisterRoutes 注册 PostController 的路由
func (ctrl *PostController) RegisterRoutes(group *gin.RouterGroup) {
	posts := group.Group("/posts")
	{
		posts.POST("", ctrl.CreatePost)                    // POST /api/v1/post/posts
		posts.DELETE("/:id", ctrl.DeletePost)              // DELETE /api/v1/post/posts/:id
		posts.GET("/timeline", ctrl.GetPostsTimeline)      // GET /api/v1/post/posts/timeline
		posts.GET("/mine", ctrl.GetUserPosts)              // GET /api/v1/post/posts/mine
		posts.GET("/by-author", ctrl.ListPostsByUserID)    // GET /api/v1/post/posts/by-author (路径已修改)
		posts.GET("/:post_id", ctrl.GetPostDetailByPostID) // GET /api/v1/post/posts/:post_id
	}
}
