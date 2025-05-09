package service

import (
	"context"
	"errors" // 用于错误检查，例如 errors.Is
	"fmt"
	"github.com/Xushengqwer/go-common/constants" // 导入包含 Context Key 的包
	// 引入项目内的包和公共模块
	"github.com/Xushengqwer/go-common/commonerrors" // 假设用于 ErrNotFound 等
	"github.com/Xushengqwer/go-common/core"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/models/entities"
	"github.com/Xushengqwer/post_service/models/enums"
	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/mq/producer"
	"github.com/Xushengqwer/post_service/repo/mysql"
	"github.com/Xushengqwer/post_service/repo/redis"
)

// PostService 定义了处理帖子核心业务逻辑的接口。
// - 它作为控制器层和数据仓库层之间的桥梁，封装业务规则。
type PostService interface {
	// CreatePost 处理用户发布新帖子的业务流程。
	// - 接收 DTO 作为输入，封装了创建帖子所需的所有信息。
	// - 负责将帖子及其详情原子性地写入数据库。
	// - 成功创建后，异步触发 Kafka 事件通知审核服务。
	// - 返回 VO，包含成功创建的帖子的基本信息。
	CreatePost(ctx context.Context, req *dto.CreatePostRequest) (*vo.PostResponse, error)

	// DeletePost 处理用户或管理员下架/删除帖子的操作。
	// - 接收帖子 ID 作为输入。
	// - 执行数据库软删除（帖子和详情），确保操作的原子性。
	// - 异步触发 Kafka 事件通知下游服务（如搜索引擎）进行数据同步。
	DeletePost(ctx context.Context, id uint64) error

	// ListPostsByUserID 获取指定用户发布的帖子列表（游标分页）。
	// - 接收包含 userID, 可选cursor, pageSize 的 DTO。
	// - 设计用于支持无限滚动或分页加载场景，如用户个人主页。
	// - 调用仓库层实现具体的游标查询逻辑。
	// - 将查询结果转换为前端展示所需的 VO 列表。
	ListPostsByUserID(ctx context.Context, req *dto.ListPostsByUserIDRequest) (*vo.ListPostsByUserIDResponse, error)

	// GetPostDetailByPostID 获取单个帖子的详细信息。
	// - 接收帖子 ID 作为输入。
	// - 从数据库获取帖子详情数据。
	// - 异步增加帖子的浏览计数（如果用户已登录）。
	// - 将实体数据转换为前端展示所需的 VO。
	GetPostDetailByPostID(ctx context.Context, postID uint64) (*vo.PostDetailResponse, error)
}

// postService 是 PostService 接口的具体实现。
type postService struct {
	// 依赖注入的仓库和外部服务实例
	postRepo       mysql.PostRepository       // 负责帖子的 MySQL 操作
	postDetailRepo mysql.PostDetailRepository // 负责帖子详情的 MySQL 操作
	postViewRepo   redis.PostViewRepository   // 负责帖子浏览量相关的 Redis 操作
	db             *gorm.DB                   // GORM 数据库实例，主要用于事务管理
	kafkaSvc       *producer.KafkaProducer    // Kafka 生产者，用于发送异步消息
	logger         *core.ZapLogger            // 日志记录器，用于记录关键信息和错误
}

// NewPostService 是 postService 的构造函数，通过依赖注入初始化服务实例。
// - 这种方式便于单元测试和组件替换。
func NewPostService(db *gorm.DB, postRepo mysql.PostRepository, postDetailRepo mysql.PostDetailRepository, postViewRepo redis.PostViewRepository, kafkaSvc *producer.KafkaProducer, logger *core.ZapLogger) PostService {
	return &postService{
		postRepo:       postRepo,
		postDetailRepo: postDetailRepo,
		db:             db,
		postViewRepo:   postViewRepo,
		kafkaSvc:       kafkaSvc,
		logger:         logger,
	}
}

// CreatePost 实现创建帖子及其详情的逻辑。
func (s *postService) CreatePost(ctx context.Context, req *dto.CreatePostRequest) (*vo.PostResponse, error) {
	// 声明变量用于在事务成功后访问新创建的实体，以便发送 Kafka 消息和构建响应。
	var createdPost *entities.Post
	var createdDetail *entities.PostDetail

	// 使用 GORM 的 Transaction 方法来确保帖子和详情的创建具有原子性。
	// 传入的 ctx 会被 GORM 用来将事务 tx 与上下文关联。
	// 仓库方法若使用 db.WithContext(ctx)，会自动使用此事务 tx。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// --- 在事务内执行 ---
		// 1. 准备帖子实体数据
		post := &entities.Post{
			Title:          req.Title,
			AuthorID:       req.AuthorID,
			AuthorAvatar:   req.AuthorAvatar,
			AuthorUsername: req.AuthorUsername,
			Status:         enums.Pending, // 新帖子默认为待审核状态
			ViewCount:      0,             // 初始浏览量为 0
			// BaseModel (ID, CreatedAt, UpdatedAt) 由 GORM 自动填充
		}
		// 调用仓库方法创建帖子记录，并传入事务对象 tx
		if repoErr := s.postRepo.CreatePost(ctx, tx, post); repoErr != nil {
			// 如果仓库方法返回错误，直接返回错误，GORM 会自动回滚事务。
			// 使用 %w 包装错误，保留原始错误信息。
			return fmt.Errorf("创建帖子失败: %w", repoErr)
		}
		createdPost = post // 在事务作用域外需要用到 post，先保存其引用

		// 2. 准备帖子详情实体数据
		postDetail := &entities.PostDetail{
			PostID:         post.ID, // 关键：使用刚创建帖子的 ID 作为外键
			Content:        req.Content,
			PricePerUnit:   req.PricePerUnit,
			ContactQRCode:  req.ContactQRCode,
			AuthorID:       req.AuthorID,       // 冗余存储作者信息，用于详情页直接展示
			AuthorAvatar:   req.AuthorAvatar,   // 避免查询用户服务
			AuthorUsername: req.AuthorUsername, // 需要通过机制（如MQ）保持与用户服务同步
			// BaseModel (ID, CreatedAt, UpdatedAt) 由 GORM 自动填充
		}
		// 调用仓库方法创建帖子详情记录，并传入事务对象 tx
		if repoErr := s.postDetailRepo.CreatePostDetail(ctx, tx, postDetail); repoErr != nil {
			return fmt.Errorf("创建帖子详情失败: %w", repoErr)
		}
		createdDetail = postDetail // 保存引用

		// 事务闭包成功执行完毕，返回 nil，GORM 将自动提交事务。
		return nil
	})

	// 检查事务的最终结果。
	if err != nil {
		// 如果事务失败（上面的闭包返回了 error），记录错误并向上层返回。
		s.logger.Error("创建帖子事务失败", zap.Error(err))
		return nil, err
	}

	// --- 事务成功提交后执行 ---

	// 3. 准备要发送到 Kafka 的事件数据。
	//    包含帖子和详情的关键信息，供下游服务（如审核服务）使用。
	postEvent := producer.PostEvent{
		ID:             createdPost.ID,
		Title:          createdPost.Title,
		Content:        createdDetail.Content,
		AuthorID:       createdPost.AuthorID,
		AuthorAvatar:   createdPost.AuthorAvatar,
		AuthorUsername: createdPost.AuthorUsername,
		Status:         createdPost.Status,
		ViewCount:      createdPost.ViewCount,
		PricePerUnit:   createdDetail.PricePerUnit,
		ContactQRCode:  createdDetail.ContactQRCode,
	}

	// 4. 异步发送 Kafka 事件。
	//    使用 goroutine 是为了避免阻塞当前 API 请求的响应。
	//    使用 context.Background() 是因为这个后台任务的生命周期独立于原始请求的 context。
	//    将 event 作为参数传递给 goroutine，避免闭包捕获变量可能带来的问题。
	go func(event producer.PostEvent) {
		bgCtx := context.Background() // 创建独立的后台上下文
		// 考虑为 Kafka 调用设置独立的超时
		// kafkaCtx, cancel := context.WithTimeout(bgCtx, 10*time.Second)
		// defer cancel()
		if kafkaErr := s.kafkaSvc.SendPostAuditEvent(bgCtx, event); kafkaErr != nil {
			// 记录发送失败的错误，但不影响主流程的成功返回。
			// 需要有监控或重试机制来处理潜在的发送失败。
			s.logger.Error("发送 Kafka 审核事件失败", zap.Error(kafkaErr), zap.Uint64("post_id", event.ID))
		}
	}(postEvent)

	// 5. 构造并返回给控制器的成功响应数据 (VO)。
	return &vo.PostResponse{
		ID:             createdPost.ID,
		Title:          createdPost.Title,
		Status:         uint(createdPost.Status), // 将枚举转换为 uint 以便 JSON 序列化
		ViewCount:      createdPost.ViewCount,
		AuthorID:       createdPost.AuthorID,
		AuthorAvatar:   createdPost.AuthorAvatar,
		AuthorUsername: createdPost.AuthorUsername,
		CreatedAt:      createdPost.CreatedAt,
		UpdatedAt:      createdPost.UpdatedAt,
	}, nil
}

// DeletePost 实现帖子的软删除逻辑。
func (s *postService) DeletePost(ctx context.Context, id uint64) error {
	// 使用 GORM Transaction 确保帖子和详情的软删除操作是原子的。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 调用仓库层软删除帖子记录，并传入事务对象 tx
		//    现在仓库方法直接接受 tx 作为其操作的数据库句柄。
		if repoErr := s.postRepo.DeletePost(ctx, tx, id); repoErr != nil {
			// 如果希望“删除一个不存在的帖子”不视为错误（幂等性），
			// 并且仓库层已调整为在未找到时返回特定错误 (如 commonerrors.ErrRepoNotFound)，
			// 可以在这里检查 repoErr 的类型。
			// 例如:
			// if errors.Is(repoErr, commonerrors.ErrRepoNotFound) {
			//    // log.Printf("Post with id %d not found, but treating as success for idempotency.", id)
			//    return nil // 事务继续
			// }
			return fmt.Errorf("软删除帖子失败: %w", repoErr)
		}

		// 2. 调用仓库层软删除对应的帖子详情记录，并传入事务对象 tx
		if repoErr := s.postDetailRepo.DeletePostDetailByPostID(ctx, tx, id); repoErr != nil {
			// 同样可以进行幂等性检查
			// if errors.Is(repoErr, commonerrors.ErrRepoNotFound) {
			//    // log.Printf("Post detail for post_id %d not found, but treating as success for idempotency.", id)
			//    return nil // 事务继续
			// }
			return fmt.Errorf("软删除帖子详情失败: %w", repoErr)
		}

		// 事务成功完成。
		return nil
	})

	// 检查事务结果。
	if err != nil {
		// 记录事务层面的失败。
		s.logger.Error("删除帖子事务失败", zap.Error(err), zap.Uint64("post_id", id))
		return err
	}

	// 3. 异步发送 Kafka 删除事件。
	//    通知下游服务（如搜索索引）该帖子已被删除。
	//    使用 context.Background()，原因同 CreatePost。
	go func(postID uint64) {
		bgCtx := context.Background()
		if kafkaErr := s.kafkaSvc.SendPostDeleteEvent(bgCtx, postID); kafkaErr != nil {
			s.logger.Error("发送 Kafka 删除事件失败", zap.Error(kafkaErr), zap.Uint64("post_id", postID))
		}
	}(id)

	return nil // 操作成功
}

// ListPostsByUserID 实现获取用户帖子列表的逻辑。
func (s *postService) ListPostsByUserID(ctx context.Context, req *dto.ListPostsByUserIDRequest) (*vo.ListPostsByUserIDResponse, error) {
	// 业务逻辑相对简单：直接调用仓库层获取数据。
	posts, nextCursor, err := s.postRepo.GetPostsByUserIDCursor(ctx, req.UserID, req.Cursor, req.PageSize)
	if err != nil {
		s.logger.Error("获取用户帖子列表失败", zap.Error(err), zap.String("userID", req.UserID))
		// 可以考虑根据 err 类型返回不同的错误，或直接返回。
		return nil, err
	}

	// 将从数据库获取的实体列表 (posts) 转换为前端需要的 VO 列表 (postResponses)。
	// 这是典型的 DTO -> Service -> Repository -> Entity -> Service -> VO 的转换流程。
	postResponses := make([]*vo.PostResponse, 0, len(posts)) // 预分配容量
	for _, post := range posts {
		postResponses = append(postResponses, &vo.PostResponse{
			ID:             post.ID,
			Title:          post.Title,
			Status:         uint(post.Status),
			ViewCount:      post.ViewCount,
			AuthorID:       post.AuthorID,
			AuthorAvatar:   post.AuthorAvatar,
			AuthorUsername: post.AuthorUsername,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
		})
	}

	// 构造最终的响应结构体。
	response := &vo.ListPostsByUserIDResponse{
		Posts:      postResponses,
		NextCursor: nextCursor, // 将仓库层返回的下一页游标传递给上层
	}

	return response, nil
}

// GetPostDetailByPostID 实现获取帖子详情的逻辑。
func (s *postService) GetPostDetailByPostID(ctx context.Context, postID uint64) (*vo.PostDetailResponse, error) {
	// 1. 调用仓库层获取帖子详情数据。
	postDetail, err := s.postDetailRepo.GetPostDetailByPostID(ctx, postID)
	if err != nil {
		// 处理可能的错误，例如记录未找到。
		if errors.Is(err, commonerrors.ErrRepoNotFound) { // 假设有 ErrRepoNotFound
			s.logger.Warn("尝试获取不存在的帖子详情", zap.Uint64("postID", postID))
		} else {
			s.logger.Error("获取帖子详情失败", zap.Error(err), zap.Uint64("postID", postID))
		}
		return nil, err // 将错误传递给上层
	}

	// 2. 尝试从请求上下文中获取 UserID。
	//    UserID 由上游中间件（如 UserContextMiddleware）注入。
	//    获取 UserID 的目的是为了执行下面的浏览量增加操作。
	userIDVal := ctx.Value(constants.UserIDKey) // 使用常量或类型安全的 key 更佳
	if userIDVal == nil {
		// 如果上下文中没有 UserID（例如未登录用户访问），则不增加浏览量，但仍然返回帖子详情。
		s.logger.Info("未找到 UserID，跳过增加浏览量", zap.Uint64("postID", postID))
	} else {
		// 进行类型断言，确保 UserID 是期望的 string 类型。
		userIDStr, ok := userIDVal.(string)
		if !ok || userIDStr == "" {
			// 类型错误或 UserID 为空，记录警告，但不阻塞主流程。
			s.logger.Warn("从 context 获取 UserID 失败或为空", zap.Any("userIDValue", userIDVal), zap.Uint64("postID", postID))
		} else {
			// 3. 异步增加帖子的浏览计数。
			//    使用 goroutine + context.Background() 执行，理由同上（不阻塞、独立生命周期）。
			//    这是典型的 "fire-and-forget" 模式，如果增加浏览量失败，主流程不受影响。
			go func(pID uint64, uID string) {
				// 仓库层的 IncrementViewCount 负责处理 Redis 操作和可能的布隆过滤器逻辑。
				if redisErr := s.postViewRepo.IncrementViewCount(context.Background(), pID, uID); redisErr != nil {
					// 记录增加浏览量失败的错误，便于监控。
					s.logger.Error("异步增加浏览量失败", // 原文 "Failed to increment view count"
						zap.Error(redisErr),
						zap.Uint64("post_id", pID),
						zap.String("user_id", uID))
				}
			}(postID, userIDStr)
		}
	}

	// 4. 将帖子详情实体转换为 VO。
	postDetailResponse := &vo.PostDetailResponse{
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

	// 5. 返回详情 VO。
	return postDetailResponse, nil
}
