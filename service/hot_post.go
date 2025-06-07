// File: service/hot_post.go
package service

import (
	"context"
	"errors" // 需要导入 errors 包
	"fmt"
	"time" // 用于 GetHotPostDetail 的异步调用超时（如果需要）

	"github.com/Xushengqwer/go-common/core"
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/repo/redis" // 包含 PostCache 和 PostViewRepository 接口
)

// PostServiceInterface 定义了处理热门帖子相关查询的业务逻辑接口。
type PostServiceInterface interface {
	GetHotPostsByCursor(ctx context.Context, lastPostID *uint64, limit int) ([]*vo.PostResponse, *uint64, error)
	GetHotPostDetail(ctx context.Context, postID uint64, userID string) (*vo.PostDetailVO, error)
}

// HotPostService 是 PostServiceInterface 的具体实现。
type HotPostService struct {
	// 修改：使用更具体的 PostCache 接口，该接口应只包含服务层所需的读取方法
	postCache    redis.Cache              // 依赖帖子缓存读取接口
	postViewRepo redis.PostViewRepository // 依赖帖子浏览和排名操作接口
	logger       *core.ZapLogger
}

// NewHotPostService (原 NewPostQueryService) 是 HotPostService 的构造函数。
func NewHotPostService(
	postCache redis.Cache, // 修改：注入 PostCache
	postViewRepo redis.PostViewRepository,
	logger *core.ZapLogger,
) *HotPostService {
	return &HotPostService{
		postCache:    postCache,
		postViewRepo: postViewRepo,
		logger:       logger,
	}
}

// GetHotPostsByCursor 实现游标方式获取热门帖子列表。
// - lastPostID: 上一页最后一条帖子的 ID，为 nil 表示首次加载。
// - limit: 希望获取的帖子数量。
// - 返回: 帖子列表, 下一页游标, 错误。
func (s *HotPostService) GetHotPostsByCursor(ctx context.Context, lastPostID *uint64, limit int) ([]*vo.PostResponse, *uint64, error) {
	var start int64 // ZSet 范围查询的起始排名 (0-based)

	if limit <= 0 { // 基本的参数校验
		s.logger.Warn("GetHotPostsByCursor: 请求的 limit 小于或等于0", zap.Int("limit", limit))
		return []*vo.PostResponse{}, nil, errors.New("limit 参数必须大于0")
	}

	if lastPostID == nil { // 首次加载
		start = 0
		s.logger.Debug("热门帖子首次加载 (游标分页)", zap.Int("limit", limit))
	} else { // 非首次加载，根据 lastPostID 计算 start
		rank, err := s.postCache.GetPostRank(ctx, *lastPostID)
		if err != nil {
			s.logger.Error("获取上一页最后帖子排名失败 (游标分页)", zap.Error(err), zap.Uint64p("lastPostID", lastPostID))
			return nil, nil, fmt.Errorf("获取帖子排名失败: %w", err)
		}
		if rank == -1 { // 游标帖子已不在榜单中
			s.logger.Warn("游标 lastPostID 已不在热榜中 (游标分页)", zap.Uint64p("lastPostID", lastPostID))
			// 可以返回特定错误提示客户端游标失效，或作为空列表处理。
			// 这里返回特定错误，让客户端决定如何响应（例如提示刷新或从头加载）。
			return nil, nil, fmt.Errorf("提供的游标帖子(ID: %d)已不在热门榜单中，请刷新", *lastPostID)
		}
		start = rank + 1 // 下一页从上一页最后一条的下一名开始
		s.logger.Debug("热门帖子分页加载", zap.Uint64p("lastPostID", lastPostID), zap.Int64("startRank", start), zap.Int("limit", limit))
	}

	stop := start + int64(limit) - 1 // 计算 ZSet 查询的结束排名

	// 从热榜 ZSet 获取指定排名范围内的帖子 ID 列表。
	postIDs, err := s.postCache.GetPostsByRange(ctx, start, stop)
	if err != nil {
		s.logger.Error("从缓存按排名范围获取帖子 ID 失败 (游标分页)", zap.Error(err), zap.Int64("start", start), zap.Int64("stop", stop))
		return nil, nil, fmt.Errorf("获取帖子 ID 列表失败: %w", err)
	}

	if len(postIDs) == 0 { // 未获取到任何 ID（可能已到达列表末尾或该范围无数据）
		s.logger.Info("按排名范围未获取到帖子 ID (游标分页)，可能已到末尾", zap.Int64("start", start), zap.Int64("stop", stop))
		return []*vo.PostResponse{}, nil, nil // 返回空列表和 nil 游标，表示没有更多数据
	}
	s.logger.Debug("成功从 ZSet 获取到帖子 ID 列表 (游标分页)", zap.Int("count", len(postIDs)))

	// 根据获取到的 postIDs 列表，从 Redis Hash 缓存中批量获取帖子实体数据。
	posts, err := s.postCache.GetPosts(ctx, postIDs)
	if err != nil {
		s.logger.Error("从缓存批量获取帖子实体失败 (游标分页)", zap.Error(err), zap.Any("postIDs", postIDs)) // 使用 zap.Any 因为 Uint64s 可能很长
		return nil, nil, fmt.Errorf("获取帖子详情失败: %w", err)
	}
	// GetPosts 可能因部分 ID 缓存未命中而返回比 postIDs 数量少的记录。
	// 游标的确定应基于从 ZSet 获取的 ID 数量。

	// 将数据库实体转换为前端视图对象 (VO)。
	postResponses := make([]*vo.PostResponse, 0, len(posts))
	for _, post := range posts { // post 是 *entities.Post
		if post == nil { // 防御性检查，尽管 GetPosts 通常不应返回nil元素
			continue
		}
		postResponses = append(postResponses, &vo.PostResponse{
			ID:             post.ID,
			Title:          post.Title,
			Status:         post.Status,
			ViewCount:      post.ViewCount, // 此 ViewCount 来自帖子 Hash 缓存，是快照值
			AuthorID:       post.AuthorID,
			AuthorAvatar:   post.AuthorAvatar,
			AuthorUsername: post.AuthorUsername,
			OfficialTag:    post.OfficialTag,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
		})
	}

	// 确定下一页的游标。
	var nextCursor *uint64
	// 如果从 ZSet 获取的 ID 数量等于请求的 limit，说明可能还有更多数据。
	// 使用 postIDs (来自ZSet) 的最后一个 ID 作为下一页的游标。
	if len(postIDs) == limit && len(postResponses) > 0 {
		// 确保 postResponses 非空才取最后一个 ID，以防 posts 列表为空（虽然理论上不应发生如果 postIDs 非空且GetPosts行为符合预期）
		// 游标应该是 postIDs 中的最后一个，因为 postResponses 可能因 GetPosts 的部分未命中而比 postIDs 短。
		lastReturnedID := postIDs[len(postIDs)-1]
		nextCursor = &lastReturnedID
		s.logger.Debug("确定下一页游标 (游标分页)", zap.Uint64("nextCursor", *nextCursor))
	} else {
		nextCursor = nil // 没有更多数据
		s.logger.Debug("已到达热门帖子列表末尾 (游标分页)")
	}

	return postResponses, nextCursor, nil
}

// GetHotPostDetail 实现获取热门帖子详情的逻辑。
// - userID 用于触发浏览量增加。如果 userID 为空字符串，通常不应增加浏览量（需在 Controller 或此处校验）。
func (s *HotPostService) GetHotPostDetail(ctx context.Context, postID uint64, userID string) (*vo.PostDetailVO, error) {
	s.logger.Debug("获取热门帖子详情", zap.Uint64("postID", postID), zap.String("userID", userID))

	// 1. 异步增加帖子的浏览计数。
	//    前提：userID 不为空时才进行计数。此校验通常在 Controller 层完成，或在此处补充。
	if userID != "" { // 确保有有效的用户ID才增加浏览量
		go func(pID uint64, uID string) {
			// 为异步 Goroutine 创建新的后台上下文，不直接使用原始请求的 ctx，以防请求提前结束。
			// 但也可以考虑设置一个合理的短超时，例如1-2秒。
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // 短超时
			defer cancel()

			if err := s.postViewRepo.IncrementViewCount(bgCtx, pID, uID); err != nil {
				s.logger.Error("异步增加热门帖子浏览量失败",
					zap.Error(err),
					zap.Uint64("post_id", pID),
					zap.String("user_id", uID))
			} else {
				s.logger.Debug("成功触发异步增加热门帖子浏览量", zap.Uint64("post_id", pID), zap.String("user_id", uID))
			}
		}(postID, userID)
	} else {
		s.logger.Debug("未提供 userID，跳过增加浏览量步骤", zap.Uint64("postID", postID))
	}

	// 2. 从 Redis 缓存中获取帖子详情。
	//    s.postCache.GetPostDetail 应该返回 *vo.PostDetailResponse
	postDetailVO, err := s.postCache.GetPostDetail(ctx, postID)
	if err != nil {
		// 直接返回从 cache 层获取的错误，包括 myErrors.ErrCacheMiss
		s.logger.Warn("从缓存获取帖子详情失败", zap.Error(err), zap.Uint64("postID", postID))
		return nil, err // 直接传递错误，上层 Controller 处理回源或具体错误响应
	}

	s.logger.Debug("成功从缓存获取帖子详情", zap.Uint64("postID", postID))

	// 3. 返回详情 VO。
	return postDetailVO, nil
}
