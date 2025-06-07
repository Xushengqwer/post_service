package controller

import (
	"net/http"
	"strconv"

	"github.com/Xushengqwer/go-common/response" // 假设这是你的通用响应包
	"github.com/gin-gonic/gin"

	"github.com/Xushengqwer/post_service/models/vo" // 假设 vo 包含响应结构体，如 PostResponse, PostDetailResponse 等
	"github.com/Xushengqwer/post_service/service"
)

// HotPostController 定义热门帖子控制器的结构体
type HotPostController struct {
	postService service.PostServiceInterface // 服务层接口
}

// NewHotPostController 构造函数，注入服务层依赖
func NewHotPostController(postService service.PostServiceInterface) *HotPostController {
	return &HotPostController{
		postService: postService,
	}
}

// GetHotPostsByCursor 处理获取热门帖子的 HTTP 请求
// @Summary      通过游标获取热门帖子
// @Description  使用基于游标的分页方式，检索热门帖子列表。使用查询参数来传递游标和数量限制。
// @Tags         hot-posts (热门帖子)
// @Accept       json
// @Produce      json
// @Param        last_post_id query uint64 false "上一页最后一个帖子的 ID，首页省略" Format(uint64)
// @Param        limit query int true "每页帖子数量" Format(int) minimum(1)
// @Success      200 {object} vo.ListPostsByCursorResponseWrapper "热门帖子检索成功。" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的输入参数（例如，无效的 limit 或 last_post_id 格式）" // <--- 修改
// @Failure      500 {object} vo.BaseResponseWrapper "检索热门帖子时发生内部服务器错误" // <--- 修改
// @Router       /api/v1/post/hot-posts [get]
func (ctrl *HotPostController) GetHotPostsByCursor(c *gin.Context) {
	// 1. 处理 last_post_id 参数（可选）
	var lastPostID *uint64
	if lastPostIDStr := c.Query("last_post_id"); lastPostIDStr != "" {
		// 对 uint64 使用 ParseUint 并指定 bitSize 为 64
		id, err := strconv.ParseUint(lastPostIDStr, 10, 64)
		if err != nil {
			response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的 last post ID 格式")
			return
		}
		lastPostID = &id // 直接赋解析后的 uint64 的地址
	}

	// 2. 处理 limit 参数（必填）
	limitStr := c.Query("limit")
	// 如果后面使用 ShouldBindQuery，则无需检查空字符串，但这里为了清晰起见保留。
	if limitStr == "" {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "limit 是必需的")
		return
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的 limit，必须是正整数")
		return
	}

	// 3. 调用服务层获取热门帖子
	posts, nextCursor, err := ctrl.postService.GetHotPostsByCursor(c.Request.Context(), lastPostID, limit)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索热门帖子失败: "+err.Error())
		return
	}

	//  todo 有问题
	// 4. 构造响应结构体 - 如注释所述，复用 ListHotPostsByCursorResponse
	// 确保 vo.ListHotPostsByCursorResponse 结构体匹配预期的输出 {posts, next_cursor}
	responseData := vo.ListHotPostsByCursorResponse{ // 这里的业务逻辑仍然使用原始的 VO
		Posts:      posts, // 假设 GetHotPostsByCursor 返回 []*vo.PostResponse
		NextCursor: nextCursor,
	}

	// 5. 返回成功响应
	response.RespondSuccess(c, responseData, "热门帖子检索成功")
}

// GetHotPostDetail 处理获取热门帖子详情的 HTTP 请求
// @Summary      根据帖子 ID 获取热门帖子详情
// @Description  通过帖子的 ID 检索特定热门帖子的详细信息。需要在 URL 路径中提供帖子 ID，并从上下文中获取 UserID。
// @Tags         hot-posts (热门帖子)
// @Accept       json
// @Produce      json
// @Param        post_id path uint64 true "帖子 ID" Format(uint64)
// @Success      200 {object} vo.PostDetailResponseWrapper "热门帖子详情检索成功" // <--- 修改
// @Failure      400 {object} vo.BaseResponseWrapper "无效的帖子 ID 格式" // <--- 修改
// @Failure      401 {object} vo.BaseResponseWrapper "在上下文中未找到用户 ID（未授权）" // <--- 修改
// @Failure      404 {object} vo.BaseResponseWrapper "热门帖子详情未找到" // <-- 添加404情况
// @Failure      500 {object} vo.BaseResponseWrapper "检索热门帖子详情时发生内部服务器错误" // <--- 修改
// @Router       /api/v1/post/hot-posts/{post_id} [get]
func (ctrl *HotPostController) GetHotPostDetail(c *gin.Context) {
	// 1. 从 URL 参数获取帖子 ID
	postIDStr := c.Param("post_id")
	postID, err := strconv.ParseUint(postIDStr, 10, 64) // 使用 bitSize 64
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, "无效的帖子 ID 格式")
		return
	}

	// 2. 从 gin.Context 获取 UserID（假设由认证中间件设置）
	// 为方便起见使用 GetString，如果未找到或不是字符串则返回 ""
	userIDStr := c.GetString("UserID") // 假设 UserContextMiddleware 设置了 "UserID"
	if userIDStr == "" {
		// 通常，如果缺少用户上下文，应返回 401 未授权
		response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "在上下文中未找到用户 ID")
		return
	}

	// 3. 调用服务层获取热门帖子详情
	responseData, err := ctrl.postService.GetHotPostDetail(c.Request.Context(), postID, userIDStr)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrCodeServerInternal, "检索热门帖子详情失败: "+err.Error())
		return
	}
	// 检查 responseData 是否为 nil（如果服务对于未找到的情况返回 nil, nil，则可能发生）
	if responseData == nil {
		response.RespondError(c, http.StatusNotFound, response.ErrCodeClientResourceNotFound, "热门帖子详情未找到")
		return
	}

	// 5. 返回成功响应
	// 因为服务返回 *vo.PostDetailResponse，所以需要解引用 responseData
	response.RespondSuccess(c, *responseData, "热门帖子详情检索成功")
}

// RegisterRoutes 注册 HotPostController 的路由
func (ctrl *HotPostController) RegisterRoutes(group *gin.RouterGroup) {
	hotPosts := group.Group("/hot-posts") // 基础路径 /hot-posts
	{
		hotPosts.GET("", ctrl.GetHotPostsByCursor)       // GET /hot-posts
		hotPosts.GET("/:post_id", ctrl.GetHotPostDetail) // GET /hot-posts/{post_id}
	}
}
