package service

import (
	"context"
	"errors" // 用于错误检查，例如 errors.Is
	"fmt"
	"github.com/Xushengqwer/go-common/models/enums"
	"github.com/Xushengqwer/go-common/models/kafkaevents"
	"github.com/Xushengqwer/post_service/constant"
	"github.com/Xushengqwer/post_service/dependencies"
	"github.com/google/uuid"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	// 引入项目内的包和公共模块
	"github.com/Xushengqwer/go-common/commonerrors" // 假设用于 ErrNotFound 等
	"github.com/Xushengqwer/go-common/core"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/models/entities"

	"github.com/Xushengqwer/post_service/models/vo"
	"github.com/Xushengqwer/post_service/mq/producer"
	"github.com/Xushengqwer/post_service/repo/mysql"
	"github.com/Xushengqwer/post_service/repo/redis"
)

// PostService 定义了处理帖子核心业务逻辑的接口。
type PostService interface {
	// CreatePost 处理用户发布新帖子的业务流程。
	// - 接收 DTO 作为输入，封装了创建帖子所需的所有信息,包括帖子基础信息，帖子详情信息，帖子详情图
	// - 负责将帖子及其详情原子性地写入数据库。
	// - 成功创建后，异步触发 Kafka 事件通知审核服务。
	// - 返回 VO，包含成功创建的帖子的基本信息。
	CreatePost(ctx context.Context, req *dto.CreatePostRequest, imageFiles []*multipart.FileHeader) (*vo.PostDetailVO, error)

	// DeletePost 处理用户删除帖子的操作。
	// - 接收帖子 ID 作为输入。
	// - 执行数据库软删除（帖子和详情），确保操作的原子性。
	// - 异步触发 Kafka 事件通知下游服务（如搜索引擎）进行数据同步。
	DeletePost(ctx context.Context, id uint64) error

	// GetPostDetailByPostID 获取单个帖子的详细信息。
	// - 接收帖子 ID 作为输入。
	// - 从数据库获取帖子详情数据。
	// - 异步增加帖子的浏览计数（如果用户已登录）。
	// - 将实体数据转换为前端展示所需的 VO。
	GetPostDetailByPostID(ctx context.Context, postID uint64, userID string) (*vo.PostDetailVO, error)
}

// postService 是 PostService 接口的具体实现。
type postService struct {
	postRepo            mysql.PostRepository            // 负责帖子的 MySQL 操作
	postDetailRepo      mysql.PostDetailRepository      // 负责帖子详情的 MySQL 操作
	postDetailImageRepo mysql.PostDetailImageRepository // 帖子详情图的MySQL操作
	cosClient           dependencies.COSClientInterface // cos云服务依赖
	postViewRepo        redis.PostViewRepository        // 负责帖子浏览量相关的 Redis 操作
	db                  *gorm.DB                        // GORM 数据库实例，主要用于事务管理
	kafkaSvc            *producer.KafkaProducer         // Kafka 生产者，用于发送异步消息
	logger              *core.ZapLogger                 // 日志记录器，用于记录关键信息和错误
}

// NewPostService 是 postService 的构造函数，通过依赖注入初始化服务实例。
// - 这种方式便于单元测试和组件替换。
func NewPostService(db *gorm.DB, postRepo mysql.PostRepository, postDetailRepo mysql.PostDetailRepository, postDetailImageRepo mysql.PostDetailImageRepository, cosClient dependencies.COSClientInterface, postViewRepo redis.PostViewRepository, kafkaSvc *producer.KafkaProducer, logger *core.ZapLogger) PostService {
	return &postService{
		postRepo:            postRepo,
		postDetailRepo:      postDetailRepo,
		postDetailImageRepo: postDetailImageRepo,
		cosClient:           cosClient,
		db:                  db,
		postViewRepo:        postViewRepo,
		kafkaSvc:            kafkaSvc,
		logger:              logger,
	}
}

// generatePostImageObjectKey 创建一个唯一的 COS 对象键。
// 注意：这是一个简化示例。如果直接在路径中使用 userID 和 originalFilename，
// 请确保对其进行清理以防止安全问题。
func (s *postService) generatePostImageObjectKey(originalFilename string, userID string) string {
	now := time.Now()
	datePrefix := now.Format("20060102") // YYYYMMDD
	randomUUID := uuid.NewString()
	extension := strings.ToLower(filepath.Ext(originalFilename)) // 例如：".jpg", ".png"

	// 示例规则：posts/images/YYYYMMDD/userID_uuid.ext
	// 如果 userID 来自用户输入，请确保其已为路径使用进行清理。
	// 为简单起见，此处假设 userID 是安全的。
	return fmt.Sprintf("%s%s/%s_%s%s",
		constant.COSObjectKeyPrefixPostImages,
		datePrefix,
		userID, // 考虑清理或使用非用户控制的部分
		randomUUID,
		extension,
	)
}

// CreatePost 处理用户创建新帖子的请求，包括图片上传和数据库操作。
func (s *postService) CreatePost(ctx context.Context, req *dto.CreatePostRequest, imageFiles []*multipart.FileHeader) (*vo.PostDetailVO, error) {
	// 1. 首先将图片上传到 COS
	type UploadedImageInfo struct {
		ImageURL     string
		ObjectKey    string
		DisplayOrder int
	}
	uploadedImages := make([]UploadedImageInfo, 0, len(imageFiles))

	for i, fileHeader := range imageFiles {
		file, err := fileHeader.Open()
		if err != nil {
			s.logger.Error("打开图片文件以上传失败",
				zap.String("filename", fileHeader.Filename),
				zap.Error(err))
			return nil, fmt.Errorf("打开图片文件 %s 失败: %w", fileHeader.Filename, err)
		}
		// 确保文件被关闭，但 UploadFile 也需要读取它。
		// 如果读取器未完全消耗或传递到其他地方，defer 理想情况下应在 UploadFile 之后。
		// 目前，假设 UploadFile 完全处理了读取器。

		// 确定内容类型
		contentType := fileHeader.Header.Get("Content-Type")
		if contentType == "" {
			// 如果内容类型至关重要且未提供，则执行回退或报错
			// 快速测试：读取前 512 字节以检测。
			// 这意味着文件需要是可查找的或首先读入缓冲区。
			// 为简单起见，我们假设客户端会发送它，或者 COS 可以推断出来。
			// 如果没有，则使用默认值或增强此部分。
			contentType = "application/octet-stream" // 常见的默认值
			s.logger.Warn("未提供图片的内容类型，使用默认值",
				zap.String("filename", fileHeader.Filename),
				zap.String("defaultContentType", contentType))
		}

		objectKey := s.generatePostImageObjectKey(fileHeader.Filename, req.AuthorID)

		imageURL, err := s.cosClient.UploadFile(ctx, objectKey, file, fileHeader.Size, contentType)
		file.Close() // 在 UploadFile 使用完文件后关闭它。
		if err != nil {
			s.logger.Error("上传图片到 COS 失败",
				zap.String("filename", fileHeader.Filename),
				zap.String("objectKey", objectKey),
				zap.Error(err))
			// TODO：如果需要，为此请求已上传的 COS 文件实现回滚逻辑。
			return nil, fmt.Errorf("上传图片 %s 到 COS 失败: %w", fileHeader.Filename, err)
		}

		uploadedImages = append(uploadedImages, UploadedImageInfo{
			ImageURL:     imageURL,
			ObjectKey:    objectKey,
			DisplayOrder: i, // 基于前端文件列表的顺序
		})
		s.logger.Info("成功上传图片到 COS",
			zap.String("filename", fileHeader.Filename),
			zap.String("objectKey", objectKey),
			zap.String("imageURL", imageURL))
	}

	// 2. 在事务中执行数据库操作
	var createdPost *entities.Post
	var createdDetail *entities.PostDetail
	var createdDbImages []*entities.PostDetailImage // 存储数据库图片实体以用于VO

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 2.1 创建 Post 实体
		post := &entities.Post{
			Title:          req.Title,
			AuthorID:       req.AuthorID,
			AuthorAvatar:   req.AuthorAvatar,   // 假设 DTO 中有此字段
			AuthorUsername: req.AuthorUsername, // 假设 DTO 中有此字段
			Status:         enums.Pending,      // 默认为待审核
			ViewCount:      0,
			OfficialTag:    0, // 默认初始无标签
			// AuditReason 最初为空/null
		}
		if repoErr := s.postRepo.CreatePost(ctx, tx, post); repoErr != nil {
			return fmt.Errorf("创建帖子失败: %w", repoErr)
		}
		createdPost = post

		// 2.2 创建 PostDetail 实体
		postDetail := &entities.PostDetail{
			PostID:       post.ID,
			Content:      req.Content,
			PricePerUnit: req.PricePerUnit,
			ContactInfo:  req.ContactInfo, // 确保 DTO 字段名匹配 (旧代码中为 ContactQRCode)
		}
		if repoErr := s.postDetailRepo.CreatePostDetail(ctx, tx, postDetail); repoErr != nil {
			return fmt.Errorf("创建帖子详情失败: %w", repoErr)
		}
		createdDetail = postDetail

		// 2.3 创建 PostDetailImage 实体
		if len(uploadedImages) > 0 {
			dbImagesToCreate := make([]*entities.PostDetailImage, len(uploadedImages))
			for i, imgInfo := range uploadedImages {
				dbImagesToCreate[i] = &entities.PostDetailImage{
					PostDetailID: createdDetail.ID,
					ImageURL:     imgInfo.ImageURL,
					ObjectKey:    imgInfo.ObjectKey,
					DisplayOrder: imgInfo.DisplayOrder,
				}
			}
			if repoErr := s.postDetailImageRepo.BatchCreatePostDetailImages(ctx, tx, dbImagesToCreate); repoErr != nil {
				return fmt.Errorf("创建帖子详情图片失败: %w", repoErr)
			}
			createdDbImages = dbImagesToCreate
		}
		return nil // 提交事务
	})

	if err != nil {
		s.logger.Error("创建帖子事务失败", zap.Error(err))
		// todo  后续考虑解决孤立图片的问题
		// 如果数据库事务在 COS 图片上传成功后失败，这些图片将成为 COS 中的孤立文件。
		// 如果需要严格的原子性，请为 `uploadedImages` 实现从 COS 清理的逻辑。
		// 对 uploadedImages 中的每个 img，调用 s.cosClient.DeleteObject(context.Background(), img.ObjectKey)
		// 此清理操作应记录其自身的错误，但不应掩盖原始的数据库错误。
		for _, imgInfo := range uploadedImages {
			s.logger.Warn("由于数据库事务失败，尝试清理孤立的 COS 文件", zap.String("objectKey", imgInfo.ObjectKey))
			if cleanupErr := s.cosClient.DeleteObject(context.Background(), imgInfo.ObjectKey); cleanupErr != nil {
				s.logger.Error("清理孤立的 COS 文件失败", zap.String("objectKey", imgInfo.ObjectKey), zap.Error(cleanupErr))
			}
		}
		return nil, err
	}

	// --- 事务成功 ---

	// 3. 异步发送 Kafka 事件
	// todo 注意目前审核服务尚未加入图片审核，成本过高，仅仅是发送到审核服务保持数据完整性

	// 将数据库实体图片列表转换为 Kafka 事件所需的图片数据列表
	kafkaImagesData := make([]kafkaevents.ImageEventData, 0, len(createdDbImages))
	for _, dbImg := range createdDbImages {
		if dbImg == nil { // 安全检查
			continue
		}
		kafkaImagesData = append(kafkaImagesData, kafkaevents.ImageEventData{
			ImageURL:     dbImg.ImageURL,
			ObjectKey:    dbImg.ObjectKey,
			DisplayOrder: dbImg.DisplayOrder,
		})
	}

	postDataForKafka := kafkaevents.PostData{
		ID:           createdPost.ID,
		Title:        createdPost.Title,
		Content:      createdDetail.Content,
		AuthorID:     createdPost.AuthorID,
		AuthorAvatar: createdPost.AuthorAvatar,

		AuthorUsername: createdPost.AuthorUsername,
		Status:         createdPost.Status,
		ViewCount:      createdPost.ViewCount,
		OfficialTag:    createdPost.OfficialTag,
		PricePerUnit:   createdDetail.PricePerUnit,

		ContactInfo: createdDetail.ContactInfo, // 映射到 detail 中的 ContactInfo
		CreatedAt:   createdPost.CreatedAt.UnixMilli(),
		UpdatedAt:   createdPost.UpdatedAt.UnixMilli(),
		Images:      kafkaImagesData,
	}

	go func(pd kafkaevents.PostData) {
		bgCtx := context.Background() // 为后台 goroutine 创建新的上下文
		if kafkaErr := s.kafkaSvc.SendPostPendingAuditEvent(bgCtx, pd); kafkaErr != nil {
			s.logger.Error("发送 Kafka 帖子待审核事件失败", zap.Error(kafkaErr), zap.Uint64("post_id", pd.ID))
		} else {
			s.logger.Info("成功发送 Kafka 帖子待审核事件", zap.Uint64("post_id", pd.ID))
		}
	}(postDataForKafka)

	// 4. 构建并返回 PostDetailVO
	voImages := make([]vo.PostImageVO, len(createdDbImages))
	for i, dbImg := range createdDbImages {
		voImages[i] = vo.PostImageVO{
			ImageURL:     dbImg.ImageURL,
			DisplayOrder: dbImg.DisplayOrder,
			ObjectKey:    dbImg.ObjectKey, // 假设VO中也需要ObjectKey
		}
	}

	return &vo.PostDetailVO{
		ID:             createdPost.ID,
		CreatedAt:      createdPost.CreatedAt,
		UpdatedAt:      createdPost.UpdatedAt,
		Title:          createdPost.Title,
		AuthorID:       createdPost.AuthorID,
		AuthorAvatar:   createdPost.AuthorAvatar,
		AuthorUsername: createdPost.AuthorUsername,
		ViewCount:      createdPost.ViewCount,
		OfficialTag:    createdPost.OfficialTag,
		Content:        createdDetail.Content,
		PricePerUnit:   createdDetail.PricePerUnit,
		ContactInfo:    createdDetail.ContactInfo,
		Images:         voImages,
	}, nil
}

// DeletePost 实现帖子的软删除逻辑。
func (s *postService) DeletePost(ctx context.Context, postID uint64) error {
	var actualPostDetailID uint64

	// 1. 尝试获取帖子详情，以得到其 PostDetail.ID (即 actualPostDetailID)
	postDetail, repoErr := s.postDetailRepo.GetPostDetailByPostID(ctx, postID)
	if repoErr != nil {
		if errors.Is(repoErr, gorm.ErrRecordNotFound) {
			// 帖子详情不存在，这可能是正常情况（例如已被删除或从未创建）。
			// 这种情况下，我们不需要删除帖子详情图或帖子详情本身。
			s.logger.Info("删除帖子：未找到关联的帖子详情，将尝试删除帖子基础信息（正常情况是帖子基础信息也不存在）",
				zap.Uint64("post_id", postID))
		} else {
			// 其他查询错误
			s.logger.Error("删除帖子：获取帖子详情失败", zap.Error(repoErr), zap.Uint64("post_id", postID))
			return fmt.Errorf("获取帖子详情失败: %w", repoErr)
		}
	}

	// 使用 GORM Transaction 确保所有数据库操作是原子的。
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 获取到帖子详情的主键ID
		if postDetail != nil {
			actualPostDetailID = postDetail.ID

			// TODO: 如果需要删除COS中的图片文件：
			// 在这里（软删除数据库记录之前），调用COS的接口方法，删除在COS中的图片文件

			// 2. (软)删除对应的帖子详情图 (使用 actualPostDetailID)
			if repoErr := s.postDetailImageRepo.DeleteImagesByPostDetailID(ctx, tx, actualPostDetailID); repoErr != nil {
				s.logger.Error("删除帖子：软删除帖子详情图失败",
					zap.Uint64("post_detail_id", actualPostDetailID),
					zap.Error(repoErr))
				return fmt.Errorf("软删除帖子详情图失败: %w", repoErr)
			}

			// 3. (软)删除对应的帖子详情记录 (使用 postID)
			if repoErr := s.postDetailRepo.DeletePostDetailByPostID(ctx, tx, postID); repoErr != nil {
				s.logger.Error("删除帖子：软删除帖子详情失败",
					zap.Uint64("post_id", postID),
					zap.Error(repoErr))
				return fmt.Errorf("软删除帖子详情失败: %w", repoErr)
			}
		}

		// 4. (软)删除帖子主记录
		if repoErr := s.postRepo.DeletePost(ctx, tx, postID); repoErr != nil {
			s.logger.Error("删除帖子：软删除帖子主记录失败",
				zap.Uint64("post_id", postID),
				zap.Error(repoErr))
			return fmt.Errorf("软删除帖子主记录失败: %w", repoErr)
		}

		// 事务成功完成。
		return nil
	})

	// 检查事务结果。
	if err != nil {
		// 事务层面的失败已在上面处理或由GORM Transaction函数返回时记录。
		// 此处无需重复记录，直接返回错误。
		// s.logger.Error("删除帖子事务失败", zap.Error(err), zap.Uint64("post_id", postID)) // 这句可以去掉，因为错误已从事务闭包中返回
		return err
	}

	// TODO: （事务成功后）异步删除COS中的图片文件。

	// 5. 异步发送 Kafka 删除事件。
	go func(postIDToNotify uint64) {
		bgCtx := context.Background()
		if kafkaErr := s.kafkaSvc.SendPostDeleteEvent(bgCtx, postIDToNotify); kafkaErr != nil {
			s.logger.Error("发送 Kafka 删除事件失败", zap.Error(kafkaErr), zap.Uint64("post_id", postIDToNotify))
		} else {
			s.logger.Info("成功发送 Kafka 删除事件", zap.Uint64("post_id", postIDToNotify))
		}
	}(postID) // 使用原始传入的 postID

	s.logger.Info("帖子及其关联数据（软）删除请求处理完成", zap.Uint64("post_id", postID))
	return nil
}

// GetPostDetailByPostID 实现获取帖子详情的逻辑，并接收 UserID。
func (s *postService) GetPostDetailByPostID(ctx context.Context, postID uint64, userID string) (*vo.PostDetailVO, error) {
	s.logger.Debug("从数据库获取帖子详情", zap.Uint64("postID", postID), zap.String("userID", userID))

	// 1. 从数据库获取 Post 核心数据
	post, err := s.postRepo.GetPostByID(ctx, postID)
	if err != nil {
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			s.logger.Warn("帖子核心数据未找到", zap.Uint64("postID", postID), zap.Error(err))
		} else {
			s.logger.Error("获取帖子核心数据失败", zap.Error(err), zap.Uint64("postID", postID))
		}
		return nil, err // 返回错误
	}

	// 2. 获取帖子详情数据
	postDetail, err := s.postDetailRepo.GetPostDetailByPostID(ctx, postID)
	if err != nil {
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			s.logger.Warn("尝试获取不存在的帖子详情", zap.Uint64("postID", postID))
		} else {
			s.logger.Error("获取帖子详情失败", zap.Error(err), zap.Uint64("postID", postID))
		}
		return nil, err // 返回错误
	}

	// 2. 获取帖子详情数据
	postDetailImages, err := s.postDetailImageRepo.GetImagesByPostDetailID(ctx, postDetail.ID)
	if err != nil {
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			s.logger.Warn("尝试获取不存在的帖子详情图", zap.Uint64("postID", postID))
		} else {
			s.logger.Error("获取帖子详情图失败", zap.Error(err), zap.Uint64("postID", postID))
		}
		return nil, err // 返回错误
	}

	// 3. 检查传入的 UserID 是否为空。
	if userID == "" {
		// 如果 UserID 为空（例如未登录用户访问），则记录日志并跳过增加浏览量。
		s.logger.Warn("未提供 UserID，跳过增加浏览量", zap.Uint64("postID", postID))
	} else {
		// 4. 如果 UserID 存在，则异步增加帖子的浏览计数。
		go func(pID uint64, uID string) {
			// 使用独立的 context.Background()，因为增加浏览量操作不应阻塞主流程，
			// 并且其生命周期独立于原始请求。
			if redisErr := s.postViewRepo.IncrementViewCount(context.Background(), pID, uID); redisErr != nil {
				// 记录增加浏览量失败的错误，便于监控。
				s.logger.Error("异步增加浏览量失败",
					zap.Error(redisErr),
					zap.Uint64("post_id", pID),
					zap.String("user_id", uID))
			} else {
				s.logger.Debug("成功触发异步增加浏览量", zap.Uint64("post_id", pID), zap.String("user_id", uID))
			}
		}(postID, userID)
	}

	// 5. 组装并返回详情 VO。
	postDetailResponse := &vo.PostDetailVO{
		ID:             post.ID,
		Title:          post.Title,
		ViewCount:      post.ViewCount, // 注意：这里显示的是数据库中的浏览量，而不是实时增加后的。
		OfficialTag:    post.OfficialTag,
		AuthorID:       post.AuthorID,
		AuthorAvatar:   post.AuthorAvatar,
		AuthorUsername: post.AuthorUsername,
		CreatedAt:      post.CreatedAt,
		UpdatedAt:      post.UpdatedAt,
		Content:        postDetail.Content,
		PricePerUnit:   postDetail.PricePerUnit,
		ContactInfo:    postDetail.ContactInfo,
		Images:         vo.NewPostImageVOsFromEntities(postDetailImages),
	}

	return postDetailResponse, nil
}
