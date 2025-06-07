package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/go-common/core" // 导入日志库
	"github.com/Xushengqwer/go-common/models/enums"
	"github.com/Xushengqwer/post_service/mq/producer"
	"go.uber.org/zap" // 导入 zap
	"gorm.io/gorm"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/repo/mysql"
)

// PostAdminService 定义帖子管理员服务的接口。
// - 封装管理员对帖子的管理操作，如审核、查询、设置标签和删除。
type PostAdminService interface {
	// AuditPost 处理管理员审核帖子的请求。
	// - 内部调用仓库层更新状态和可选的原因。
	AuditPost(ctx context.Context, req *dto.AuditPostRequest) error

	// ListPostsByCondition 按条件分页查询帖子列表。
	// - 供管理后台使用，直接将 DTO 传递给仓库层。
	ListPostsByCondition(ctx context.Context, req *dto.ListPostsByConditionRequest) (*vo.ListPostsAdminByConditionResponse, error)

	// UpdateOfficialTag 处理管理员更新帖子官方标签的请求。
	// - 调用仓库层执行实际的数据库更新。
	UpdateOfficialTag(ctx context.Context, req *dto.UpdateOfficialTagRequest) error

	// DeletePostByAdmin 处理管理员删除帖子的请求。
	// - 执行软删除操作。
	// - 记录管理员操作日志。
	DeletePostByAdmin(ctx context.Context, postID uint64, adminUserID string) error
}

// postAdminService 是 PostAdminService 接口的实现。
type postAdminService struct {
	postAdminRepo  mysql.PostAdminRepository
	postRepo       mysql.PostRepository
	postDetailRepo mysql.PostDetailRepository
	logger         *core.ZapLogger
	db             *gorm.DB
	kafkaSvc       *producer.KafkaProducer // Kafka 生产者，用于发送异步消息
}

// NewPostAdminService 初始化帖子管理员服务。
func NewPostAdminService(
	postAdminRepo mysql.PostAdminRepository,
	postRepo mysql.PostRepository,
	postDetailRepo mysql.PostDetailRepository,
	logger *core.ZapLogger,
	db *gorm.DB,
	kafkaSvc *producer.KafkaProducer,
) PostAdminService {
	return &postAdminService{
		postAdminRepo:  postAdminRepo,
		postRepo:       postRepo,
		postDetailRepo: postDetailRepo,
		logger:         logger,
		db:             db,
		kafkaSvc:       kafkaSvc,
	}
}

// AuditPost 实现审核帖子的逻辑。
// - 将 DTO 中的 Reason 转换为 sql.NullString 再传递给仓库层。
func (s *postAdminService) AuditPost(ctx context.Context, req *dto.AuditPostRequest) error {
	var auditReason sql.NullString
	// 只有当状态是“拒绝”且 DTO 中提供了非空原因时，才设置 Reason。
	if req.Status == enums.Rejected && req.Reason != "" {
		auditReason = sql.NullString{String: req.Reason, Valid: true}
	} else {
		auditReason = sql.NullString{Valid: false} // 其他情况，数据库存 NULL
	}

	// 调用仓库层更新状态和原因。
	err := s.postAdminRepo.UpdatePostStatus(ctx, req.PostID, req.Status, auditReason)
	if err != nil {
		// 记录具体的错误日志
		logFields := []zap.Field{
			zap.Error(err),
			zap.Uint64("postID", req.PostID),
			zap.Any("status", req.Status),
		}
		if auditReason.Valid {
			logFields = append(logFields, zap.String("reason", auditReason.String))
		}
		s.logger.Error("审核帖子时调用仓库层失败", logFields...)

		// 可以根据错误类型决定返回给上层的错误信息
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			return fmt.Errorf("帖子(ID: %d)未找到: %w", req.PostID, err)
		}
		return fmt.Errorf("审核帖子(ID: %d)失败: %w", req.PostID, err)
	}
	s.logger.Info("管理员审核帖子成功", zap.Uint64("postID", req.PostID), zap.Any("status", req.Status))
	return nil
}

// ListPostsByCondition 实现按条件查询帖子。
// - 业务逻辑简单，主要依赖仓库层查询和结果转换。
func (s *postAdminService) ListPostsByCondition(ctx context.Context, req *dto.ListPostsByConditionRequest) (*vo.ListPostsAdminByConditionResponse, error) {
	// 直接调用仓库层进行查询。
	posts, total, err := s.postAdminRepo.ListPostsByCondition(ctx, req)
	if err != nil {
		s.logger.Error("管理员按条件查询帖子列表失败", zap.Error(err), zap.Any("request", req)) // 注意不要泄露敏感信息
		return nil, fmt.Errorf("查询帖子列表失败: %w", err)                               // 对上层隐藏具体数据库错误细节
	}

	// 将数据库实体转换为视图对象 (VO)。
	postResponses := make([]*vo.PostResponse, 0, len(posts))
	for _, post := range posts {
		postResponses = append(postResponses, &vo.PostResponse{
			ID:             post.ID,
			Title:          post.Title,
			AuthorID:       post.AuthorID,
			AuthorUsername: post.AuthorUsername,
			AuthorAvatar:   post.AuthorAvatar,
			Status:         post.Status,
			ViewCount:      post.ViewCount,
			OfficialTag:    post.OfficialTag,
			CreatedAt:      post.CreatedAt,
			UpdatedAt:      post.UpdatedAt,
		})
	}

	// 构造响应。
	response := &vo.ListPostsAdminByConditionResponse{
		Posts: postResponses,
		Total: total,
	}
	s.logger.Debug("管理员按条件查询帖子列表成功", zap.Int("count", len(posts)), zap.Int64("total", total))
	return response, nil
}

// UpdateOfficialTag 实现更新官方标签的逻辑。
func (s *postAdminService) UpdateOfficialTag(ctx context.Context, req *dto.UpdateOfficialTagRequest) error {
	// 直接调用仓库层执行更新。
	err := s.postAdminRepo.UpdateOfficialTag(ctx, req.PostID, req.OfficialTag)
	if err != nil {
		// 记录日志并根据错误类型返回。
		logFields := []zap.Field{
			zap.Error(err),
			zap.Uint64("postID", req.PostID),
			zap.Any("tag", req.OfficialTag),
		}
		s.logger.Error("更新官方标签时调用仓库层失败", logFields...)
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			return fmt.Errorf("帖子(ID: %d)未找到: %w", req.PostID, err)
		}
		return fmt.Errorf("更新帖子(ID: %d)官方标签失败: %w", req.PostID, err)
	}
	s.logger.Info("管理员更新官方标签成功", zap.Uint64("postID", req.PostID), zap.Any("tag", req.OfficialTag))
	return nil
}

// DeletePostByAdmin 实现管理员删除帖子的逻辑（包含事务和详情删除）。
func (s *postAdminService) DeletePostByAdmin(ctx context.Context, postID uint64, adminUserID string) error {
	// 1. 记录管理员操作开始日志
	s.logger.Info("管理员开始删除帖子", zap.Uint64("postID", postID), zap.String("adminUserID", adminUserID))

	// 2. 使用事务确保 Post 和 PostDetail 的删除是原子的
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 2.1. 软删除 Post 记录
		//     调用 PostRepository 的 DeletePost 方法
		if repoErr := s.postRepo.DeletePost(ctx, tx, postID); repoErr != nil {
			// 可以选择是否对 ErrRepoNotFound 进行幂等处理
			if errors.Is(repoErr, commonerrors.ErrRepoNotFound) {
				// 如果帖子已不存在，可能希望操作成功，或者至少记录 Warn 并继续尝试删除详情
				s.logger.Warn("管理员尝试删除不存在的帖子记录", zap.Uint64("postID", postID), zap.String("adminUserID", adminUserID))
				// return nil // 如果希望幂等，则返回 nil 继续
			}
			// 其他错误则回滚
			return fmt.Errorf("管理员软删除帖子记录失败: %w", repoErr)
		}

		// 2.2. 软删除 PostDetail 记录
		//     调用 PostDetailRepository 的 DeletePostDetailByPostID 方法
		if repoErr := s.postDetailRepo.DeletePostDetailByPostID(ctx, tx, postID); repoErr != nil {
			// 同样可以考虑幂等处理 ErrRepoNotFound
			if errors.Is(repoErr, commonerrors.ErrRepoNotFound) {
				s.logger.Warn("管理员删除帖子时未找到对应的详情记录（可能已被删除）", zap.Uint64("postID", postID), zap.String("adminUserID", adminUserID))
				// return nil // 如果希望幂等，则返回 nil
			}
			return fmt.Errorf("管理员软删除帖子详情失败: %w", repoErr)
		}

		// 事务闭包成功完成
		return nil
	})

	// 3. 检查事务结果
	if err != nil {
		logFields := []zap.Field{
			zap.Error(err),
			zap.Uint64("postID", postID),
			zap.String("adminUserID", adminUserID),
		}
		s.logger.Error("管理员删除帖子事务失败", logFields...)
		// 可以根据 err 类型包装返回给上层的错误
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			// 如果是因为帖子或详情一开始就不存在，可以返回不同的错误
			return fmt.Errorf("管理员尝试删除的帖子(ID: %d)或其详情未找到: %w", postID, err)
		}
		return fmt.Errorf("管理员删除帖子(ID: %d)时发生错误: %w", postID, err)
	}

	// 4. 记录操作成功日志
	s.logger.Info("管理员删除帖子成功", zap.Uint64("postID", postID), zap.String("adminUserID", adminUserID))

	//
	// 5. 触发管理员删除帖子的特定事件，如果需要的话
	go func(postID uint64) {
		bgCtx := context.Background()
		if kafkaErr := s.kafkaSvc.SendPostDeleteEvent(bgCtx, postID); kafkaErr != nil {
			s.logger.Error("发送 Kafka 删除事件失败", zap.Error(kafkaErr), zap.Uint64("post_id", postID))
		}
	}(postID)

	return nil
}
