package service

import (
	"context"
	"fmt"
	// 确保以下包路径与你的项目结构一致
	"github.com/Xushengqwer/post_service/repo/mysql" // 假设 PostRepository 定义在此

	"github.com/Xushengqwer/go-common/core" // ZapLogger 等核心组件
	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/models/vo"
	"go.uber.org/zap"
)

// PostListService 定义了与获取帖子列表相关的服务接口。
type PostListService interface {
	// GetUserPosts 获取当前登录用户自己发布的帖子列表（分页加载）。
	// - userID: 当前登录用户的ID。
	// - queryDTO: 包含筛选和分页参数的DTO。
	// - 返回: 包含帖子列表和总数的VO，以及可能发生的错误。
	GetUserPosts(ctx context.Context, userID string, queryDTO *dto.GetUserPostsRequestDTO) (*vo.ListUserPostPageVO, error)

	// GetPostsByTimeline 根据查询参数获取最新的帖子时间线列表（游标查询）。
	// - queryDTO: 包含所有查询条件和分页游标的DTO。
	// - 返回: 包含帖子列表和下一页游标的VO，以及可能发生的错误。
	GetPostsByTimeline(ctx context.Context, queryDTO *dto.TimelineQueryDTO) (*vo.PostTimelinePageVO, error)

	// ListPostsByUserID 获取指定用户发布的帖子列表（游标分页）。
	// - req: 包含 userID, 可选的游标 (cursor), 以及每页数量 (pageSize) 的DTO。
	// - 设计用于支持无限滚动或分页加载场景，例如用户个人主页。
	// - 调用仓库层实现具体的游标查询逻辑。
	// - 将查询结果转换为前端展示所需的VO列表。
	ListPostsByUserID(ctx context.Context, req *dto.ListPostsByUserIDRequest) (*vo.ListHotPostsByCursorResponse, error)
}

// postListService 提供了获取帖子列表的服务。
type postListService struct {
	logger   *core.ZapLogger
	postRepo mysql.PostRepository // 使用接口类型的仓库依赖
}

// NewPostListService 创建一个新的 PostListService 实例。
func NewPostListService(logger *core.ZapLogger, postRepo mysql.PostRepository) PostListService {
	return &postListService{
		logger:   logger,
		postRepo: postRepo,
	}
}

// GetUserPosts 获取当前登录用户发布的帖子列表（分页）。
func (s *postListService) GetUserPosts(ctx context.Context, userID string, queryDTO *dto.GetUserPostsRequestDTO) (*vo.ListUserPostPageVO, error) {
	s.logger.Info("服务层 GetUserPosts: 开始获取用户帖子列表", zap.String("userID", userID), zap.Any("queryDTO", queryDTO))

	// 1. 调用仓库层获取数据
	offset := queryDTO.GetOffset() // 假设DTO中存在 GetOffset 方法
	limit := queryDTO.GetLimit()   // 假设DTO中存在 GetLimit 方法
	posts, totalCount, err := s.postRepo.GetUserPostsByConditions(
		ctx,
		userID,
		queryDTO.OfficialTag,
		queryDTO.Title,
		queryDTO.Status,
		offset,
		limit,
	)
	if err != nil {
		s.logger.Error("服务层 GetUserPosts: 调用仓库 GetUserPostsByConditions 失败", zap.Error(err), zap.String("userID", userID))
		return nil, fmt.Errorf("获取用户帖子列表失败: %w", err)
	}

	s.logger.Info("服务层 GetUserPosts: 成功从仓库获取用户帖子数据",
		zap.String("userID", userID),
		zap.Int("retrievedCount", len(posts)), // 使用 retrievedCount 表示当前页获取到的数量
		zap.Int64("totalCount", totalCount))

	// 2. 将 entities.Post 列表转换为 vo.PostResponse 列表 ，使用我们在VO包定义的辅助转换函数。
	postResponses := vo.MapPostsToPostResponsesVO(posts)

	// 3. 构建并返回响应 VO
	responseVO := &vo.ListUserPostPageVO{
		Posts: postResponses, // 确保 Posts 字段期望的是 []*vo.PostResponse 类型
		Total: totalCount,
	}

	return responseVO, nil
}

// GetPostsByTimeline 根据查询参数获取帖子时间线列表。
func (s *postListService) GetPostsByTimeline(ctx context.Context, queryDTO *dto.TimelineQueryDTO) (*vo.PostTimelinePageVO, error) {
	s.logger.Info("服务层 GetPostsByTimeline: 开始按时间线获取帖子", zap.Any("queryDTO", queryDTO))

	// 1. 调用仓库层获取数据
	posts, nextCreatedAt, nextPostID, err := s.postRepo.GetPostsByTimeline(ctx, queryDTO)
	if err != nil {
		s.logger.Error("服务层 GetPostsByTimeline: 调用仓库 GetPostsByTimeline 失败", zap.Error(err), zap.Any("queryDTO", queryDTO))
		return nil, fmt.Errorf("获取帖子列表失败: %w", err)
	}

	s.logger.Info("服务层 GetPostsByTimeline: 成功从仓库获取帖子数据",
		zap.Int("retrievedCount", len(posts)),
		zap.Any("nextCreatedAt", nextCreatedAt),
		zap.Any("nextPostID", nextPostID),
	)

	// 2. 将 entities.Post 列表转换为相应的 VO 列表 -----[]*vo.PostResponse:
	postItems := vo.MapPostsToPostResponsesVO(posts)

	// 3. 构建并返回响应 VO
	//    确保 Posts 字段的类型与转换结果一致。
	responseVO := &vo.PostTimelinePageVO{
		Posts:         postItems,
		NextCreatedAt: nextCreatedAt,
		NextPostID:    nextPostID,
	}

	return responseVO, nil
}

// ListPostsByUserID 实现获取指定用户的帖子列表的逻辑（游标分页）。
func (s *postListService) ListPostsByUserID(ctx context.Context, req *dto.ListPostsByUserIDRequest) (*vo.ListHotPostsByCursorResponse, error) {
	s.logger.Info("服务层 ListPostsByUserID: 开始获取指定用户帖子列表 (游标分页)",
		zap.String("userID", req.UserID),
		zap.Any("cursor", req.Cursor),
		zap.Int("pageSize", req.PageSize))

	posts, nextCursor, err := s.postRepo.GetPostsByUserIDCursor(ctx, req.UserID, req.Cursor, req.PageSize)
	if err != nil {
		s.logger.Error("服务层 ListPostsByUserID: 调用仓库 GetPostsByUserIDCursor 失败", zap.Error(err), zap.String("userID", req.UserID))
		return nil, fmt.Errorf("获取用户帖子列表 (游标) 失败: %w", err)
	}

	s.logger.Info("服务层 ListPostsByUserID: 成功从仓库获取用户帖子数据 (游标分页)",
		zap.String("userID", req.UserID),
		zap.Int("retrievedCount", len(posts)),
		zap.Any("nextCursor", nextCursor))

	// 将 entities.Post 列表转换为相应的 VO 列表 -----[]*vo.PostResponse:
	postResponses := vo.MapPostsToPostResponsesVO(posts)
	// 构造最终的响应结构体。
	response := &vo.ListHotPostsByCursorResponse{
		Posts:      postResponses,
		NextCursor: nextCursor, // 将仓库层返回的下一页游标传递给上层
	}

	return response, nil
}
