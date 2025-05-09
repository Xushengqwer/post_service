package service

import (
	"context"
	"errors" // 需要导入 errors 包
	"fmt"
	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/post_service/myErrors"
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/repo/redis"
)

// PostServiceInterface 定义了处理热门帖子相关查询的业务逻辑接口。
// - 职责: 封装获取热门帖子列表（带分页）和热门帖子详情的业务流程。
// - 它主要与 Redis 缓存层 (Cache 和 PostViewRepository) 交互以提高性能。
type PostServiceInterface interface {
	// GetHotPostsByCursor 实现热门帖子信息流的游标分页加载。
	// - 使用场景: App 首页或热门板块的无限滚动列表。
	// - lastPostID (*uint64): 上一页最后一条帖子的 ID，用于定位下一页的起点。使用指针是为了区分首次加载 (nil)。
	// - limit (int): 希望获取的帖子数量。
	// - 返回:
	//    - []*vo.PostResponse: 当前页的帖子数据列表 (VO 格式)。
	//    - *uint64: 下一页的游标 ID；如果为 nil，表示没有更多数据。
	//    - error: 操作过程中发生的错误。
	GetHotPostsByCursor(ctx context.Context, lastPostID *uint64, limit int) ([]*vo.PostResponse, *uint64, error)

	// GetHotPostDetail 获取单个热门帖子的详细信息。
	// - 使用场景: 用户点击热门帖子列表项，查看详情。
	// - 负责从缓存中获取帖子详情，并在用户已登录的情况下，异步增加该帖子的浏览计数。
	// - 输入: postID (要获取详情的帖子 ID)。
	// - 返回:
	//    - *vo.PostDetailResponse: 帖子详情数据 (VO 格式)。
	//    - error: 操作过程中发生的错误 (例如缓存未命中、数据库错误等)。
	GetHotPostDetail(ctx context.Context, postID uint64) (*vo.PostDetailResponse, error)
}

// HotPostService 是 PostServiceInterface 的具体实现。
// - 它聚合了与热门帖子功能相关的依赖。
type HotPostService struct {
	// 注意：虽然 HotPostService 主要与 Redis 交互，但 New 方法签名可能仍需要 postRepo 类型匹配依赖注入框架。
	// 如果确定此服务完全不涉及 MySQL 操作，可以考虑移除 postRepo 依赖。
	cache        redis.Cache              // 核心依赖：提供热门帖子缓存（ID 列表、实体、详情）的访问接口
	postViewRepo redis.PostViewRepository // 核心依赖：提供帖子浏览量计数和排名相关操作 (如 IncrementViewCount)
	logger       *core.ZapLogger          // 新增：日志记录器 (建议注入)
}

// NewPostQueryService (或者 NewHotPostService 更贴切) 是 HotPostService 的构造函数。
// - 通过依赖注入初始化服务实例及其所需的仓库和日志组件。
func NewPostQueryService(
	cache redis.Cache,
	postViewRepo redis.PostViewRepository,
	logger *core.ZapLogger, // 新增注入
) *HotPostService { // 返回值类型建议与结构体名一致
	return &HotPostService{
		cache:        cache,
		postViewRepo: postViewRepo,
		logger:       logger, // 初始化 logger
	}
}

// GetHotPostsByCursor 实现游标方式获取热门帖子列表。
func (s *HotPostService) GetHotPostsByCursor(ctx context.Context, lastPostID *uint64, limit int) ([]*vo.PostResponse, *uint64, error) {
	var start int64 // ZSet 范围查询的起始排名 (0-based)

	// 判断是否为首次加载 (cursor 为 nil)。
	if lastPostID == nil {
		start = 0 // 首次加载，从排名 0 开始。
		s.logger.Debug("热门帖子首次加载", zap.Int("limit", limit))
	} else {
		// 非首次加载，需要根据上一次的 lastPostID 确定本次查询的起始排名。
		// 调用 cache.GetPostRank 获取 lastPostID 在热榜 ZSet 中的排名。
		rank, err := s.cache.GetPostRank(ctx, *lastPostID)
		if err != nil {
			// 处理获取排名失败的情况。
			s.logger.Error("获取上一页最后帖子排名失败", zap.Error(err), zap.Uint64p("lastPostID", lastPostID))
			return nil, nil, fmt.Errorf("获取帖子排名失败: %w", err)
		}
		// 如果返回 -1，说明 lastPostID 已不在榜单中（可能因榜单更新被移除）。
		// 这是一种边界情况，可以认为从头开始加载，或者返回错误提示客户端刷新。
		// 这里选择返回错误，让客户端知道游标失效。
		if rank == -1 {
			s.logger.Warn("游标 lastPostID 已不在热榜中", zap.Uint64p("lastPostID", lastPostID))
			return nil, nil, fmt.Errorf("提供的游标帖子(ID: %d)已不在热门榜单中", *lastPostID)
		}
		// 起始排名是上一页最后一条的排名 + 1。
		start = rank + 1
		s.logger.Debug("热门帖子分页加载", zap.Uint64p("lastPostID", lastPostID), zap.Int64("startRank", start), zap.Int("limit", limit))
	}

	// 计算 ZSet 范围查询的结束排名。
	stop := start + int64(limit) - 1

	// 调用 cache.GetPostsByRange 从热榜 ZSet 获取指定排名范围内的帖子 ID 列表。
	// 这个方法只返回 ID，不包含帖子实体。
	postIDs, err := s.cache.GetPostsByRange(ctx, start, stop)
	if err != nil {
		s.logger.Error("从缓存按排名范围获取帖子 ID 失败", zap.Error(err), zap.Int64("start", start), zap.Int64("stop", stop))
		return nil, nil, fmt.Errorf("获取帖子 ID 列表失败: %w", err)
	}

	// 如果未获取到任何 ID（可能已到达列表末尾）。
	if len(postIDs) == 0 {
		s.logger.Info("按排名范围未获取到帖子 ID，可能已到末尾", zap.Int64("start", start), zap.Int64("stop", stop))
		return []*vo.PostResponse{}, nil, nil // 返回空列表和 nil 游标
	}
	s.logger.Debug("成功从 ZSet 获取到帖子 ID 列表", zap.Int("count", len(postIDs)))

	// 根据获取到的 postIDs 列表，调用 cache.GetPosts 从 Redis Hash 缓存中批量获取帖子实体数据。
	// 这个方法返回的是 []*entities.Post 列表。
	posts, err := s.cache.GetPosts(ctx, postIDs)
	if err != nil {
		s.logger.Error("从缓存批量获取帖子实体失败", zap.Error(err), zap.Uint64s("postIDs", postIDs))
		return nil, nil, fmt.Errorf("获取帖子详情失败: %w", err)
	}
	// 注意：GetPosts 可能因为部分 ID 缓存未命中而返回比 postIDs 数量少的记录。

	// 将从缓存获取的数据库实体 (`posts`) 转换为前端展示所需的视图对象 (`postResponses`)。
	// 这一步是必需的，因为 VO 的结构可能与 Entity 不同，且可以过滤掉敏感信息。
	postResponses := make([]*vo.PostResponse, 0, len(posts))
	for _, post := range posts {
		postResponses = append(postResponses, &vo.PostResponse{
			ID:             post.ID,
			Title:          post.Title,
			Status:         uint(post.Status), // 枚举转换为 uint
			ViewCount:      post.ViewCount,    // 使用缓存实体中的 ViewCount (快照值)
			AuthorID:       post.AuthorID,
			AuthorAvatar:   post.AuthorAvatar,
			AuthorUsername: post.AuthorUsername,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
		})
	}

	// 确定下一页的游标。
	var nextCursor *uint64
	// 比较从 ZSet 获取的 ID 数量 (`postIDs`) 与请求的 limit。
	// 如果 ZSet 返回的 ID 数量等于 limit (或更多，理论上限是limit)，说明可能还有下一页。
	// 使用最后一个成功获取的 ID 作为下一页的游标。
	// **修正逻辑：** 应该比较从 ZSet 获取的 ID 数 (`len(postIDs)`) 是否达到 `limit`，
	// 而不是比较从 Hash 获取的实体数 (`len(posts)`)，因为后者可能因缓存缺失而变少。
	// **进一步修正：** `GetPostsByRange` 应该只返回 <= limit 个 ID，所以判断 `len(postIDs) == limit` 即可。
	// 并且，游标应该是 `postIDs` 里的最后一个 ID。
	if len(postIDs) == limit { // 如果获取到的 ID 数量等于请求的数量
		lastID := postIDs[len(postIDs)-1] // 获取最后一个 ID
		nextCursor = &lastID              // 将其作为下一页的游标
		s.logger.Debug("确定下一页游标", zap.Uint64("nextCursor", *nextCursor))
	} else {
		// 如果获取到的 ID 数量小于 limit，说明已经没有更多数据了。
		nextCursor = nil
		s.logger.Debug("已到达热门帖子列表末尾")
	}

	return postResponses, nextCursor, nil // 返回当前页数据和下一页游标
}

// GetHotPostDetail 实现获取热门帖子详情的逻辑。
func (s *HotPostService) GetHotPostDetail(ctx context.Context, postID uint64) (*vo.PostDetailResponse, error) {
	// 尝试从请求上下文中获取用户信息，特别是 UserID。
	// UserID 用于后续调用 IncrementViewCount 实现防刷计数。
	// 假设 userID 是通过上游中间件注入到 context 中的。
	userIDVal := ctx.Value("UserID") // 建议使用类型安全的 Context Key
	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		// 如果无法获取有效的 UserID（例如用户未登录），则无法执行浏览量计数。
		// 这里选择直接返回错误，因为增加浏览量是此接口的一个重要副作用。
		// 也可以选择跳过计数并继续获取详情，取决于业务需求。
		s.logger.Warn("无法从 context 获取有效 UserID，无法增加浏览量", zap.Uint64("postID", postID))
		// return nil, fmt.Errorf("需要用户登录才能查看详情") // 或者返回特定错误码
		// --- 为了让未登录用户也能看，我们先改为只记录日志，继续执行 ---
		s.logger.Info("未获取到 UserID，跳过浏览量增加步骤", zap.Uint64("postID", postID))
		// --- 如果上面选择返回错误，则删除下面的 else 分支 ---
	} else {
		// 如果获取到 UserID，调用 PostViewRepository 的 IncrementViewCount 方法。
		// 这个方法负责处理 Redis 计数器、排行榜 ZSet 更新以及 Bloom Filter 防刷。
		// 这里使用了一个新的后台 context 来执行这个异步操作，避免阻塞当前请求，
		// 并且允许它在原始请求结束后继续执行。
		go func(pID uint64, uID string) {
			bgCtx := context.Background()
			if err := s.postViewRepo.IncrementViewCount(bgCtx, pID, uID); err != nil {
				// 记录增加浏览量失败的错误，但不影响主流程返回帖子详情。
				s.logger.Error("异步增加热门帖子浏览量失败",
					zap.Error(err),
					zap.Uint64("post_id", pID),
					zap.String("user_id", uID))
			} else {
				s.logger.Debug("异步增加热门帖子浏览量成功", zap.Uint64("post_id", pID), zap.String("user_id", uID))
			}
		}(postID, userID)
	}

	// 调用 Cache 接口的 GetPostDetail 方法从 Redis 缓存中获取帖子详情。
	// 这个方法封装了访问 "post_detail:{id}" key 的逻辑。
	postDetail, err := s.cache.GetPostDetail(ctx, postID)
	if err != nil {
		// 处理从缓存获取详情失败的情况。
		s.logger.Error("从缓存获取帖子详情失败", zap.Error(err), zap.Uint64("postID", postID))
		// 如果是缓存未命中错误 (ErrCacheMiss)，理论上热榜帖子详情应该是在缓存中的。
		// 这可能表示缓存任务失败或数据不一致，或者请求的 ID 并非来自当前热榜。
		// 可以考虑是否需要回源到数据库查询。当前实现直接返回错误。
		if errors.Is(err, myErrors.ErrCacheMiss) {
			// TODO: 考虑添加回源逻辑？ repo.GetPostDetailByPostID(ctx, postID)
			return nil, fmt.Errorf("帖子(ID: %d)详情缓存未找到", postID) // 返回更具体的错误
		}
		return nil, fmt.Errorf("获取帖子(ID: %d)详情失败: %w", postID, err)
	}
	s.logger.Debug("成功从缓存获取帖子详情", zap.Uint64("postID", postID))

	// 将从缓存获取的详情实体 (`postDetail`) 转换为前端所需的 VO (`responseData`)。
	responseData := &vo.PostDetailResponse{
		ID:             postDetail.ID,
		PostID:         postDetail.PostID,
		Content:        postDetail.Content,
		PricePerUnit:   postDetail.PricePerUnit,
		ContactQRCode:  postDetail.ContactQRCode,
		AuthorID:       postDetail.AuthorID,
		AuthorAvatar:   postDetail.AuthorAvatar,
		AuthorUsername: postDetail.AuthorUsername,
		CreatedAt:      postDetail.CreatedAt,
		UpdatedAt:      postDetail.UpdatedAt,
	}

	return responseData, nil // 返回详情 VO
}

// 需要导入 "errors", "fmt" 包, zap 包, 以及可能的 myerrors 包
// import "errors"
// import "fmt"
// import "go.uber.org/zap"
// import "post_service/myErrors"
