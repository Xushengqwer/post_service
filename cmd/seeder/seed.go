package main

import (
	"database/sql" // 如果 AuditReason 需要设为 NULL
	"fmt"
	"github.com/Xushengqwer/go-common/models/enums"
	"math/rand"
	"strings"
	"time"

	"github.com/Xushengqwer/post_service/models/entities"

	"github.com/Xushengqwer/go-common/core" // 导入 ZapLogger
	"github.com/go-faker/faker/v4"
	"go.uber.org/zap" // 导入 zap
	"gorm.io/gorm"
)

// RunSeeder 执行数据库填充逻辑
// db: 已经初始化好的 GORM 数据库连接
// logger: ZapLogger 实例
// numPosts: 要创建的帖子数量
func RunSeeder(db *gorm.DB, logger *core.ZapLogger, numPosts int) error {
	logger.Info("开始执行数据库填充...", zap.Int("numPosts", numPosts))
	startTime := time.Now()

	// --- 1. 准备 Posts 数据 ---
	var postsToCreate []*entities.Post
	// 使用 faker 生成更多样化的数据
	for i := 0; i < numPosts; i++ {
		authorID := faker.UUIDHyphenated()   // 随机 UUID 作为 AuthorID
		status := enums.Status(rand.Intn(3)) // 随机状态 0, 1, 2
		var auditReason sql.NullString       // 准备审核原因

		// 如果是拒绝状态，可以随机生成一个原因
		if status == enums.Rejected {
			auditReason = sql.NullString{String: faker.Sentence(), Valid: true}
		}

		post := &entities.Post{
			// GORM 会自动处理 BaseModel (ID, CreatedAt, UpdatedAt, DeletedAt)
			Title:          faker.Sentence(), // 随机句子做标题
			AuthorID:       authorID,
			AuthorAvatar:   fmt.Sprintf("https://i.pravatar.cc/150?u=%s", authorID), // 随机头像 URL
			AuthorUsername: faker.Username(),                                        // 随机用户名
			Status:         status,
			ViewCount:      rand.Int63n(50000) + int64(rand.Intn(100)*500), // 随机浏览量，增加一些高浏览量的可能性
			OfficialTag:    enums.OfficialTag(rand.Intn(4)),                // 随机官方标签 0, 1, 2, 3
			AuditReason:    auditReason,                                    // 设置审核原因 (可能为 NULL)
		}
		// 设置随机的创建和更新时间 (例如过去 3 年内)
		randomDuration := time.Duration(rand.Int63n(int64(3 * 365 * 24 * time.Hour)))
		post.CreatedAt = time.Now().Add(-randomDuration)
		// 更新时间在创建时间之后随机 7 天内
		post.UpdatedAt = post.CreatedAt.Add(time.Duration(rand.Int63n(int64(7 * 24 * time.Hour))))

		postsToCreate = append(postsToCreate, post)

		if (i+1)%500 == 0 { // 每 500 条打印一次进度
			logger.Info("已准备 Post 数据...", zap.Int("count", i+1), zap.Int("total", numPosts))
		}
	}
	logger.Info("Post 数据准备完成", zap.Int("count", len(postsToCreate)))

	// --- 2. 批量插入 Post 数据 ---
	// 使用 GORM 的 CreateInBatches，它会处理主键回填
	batchSize := 100 // 每批插入 100 条
	logger.Info("开始批量插入 Post...", zap.Int("batchSize", batchSize))
	if err := db.CreateInBatches(postsToCreate, batchSize).Error; err != nil {
		logger.Error("批量插入 Post 失败", zap.Error(err))
		return fmt.Errorf("批量插入 Post 失败: %w", err)
	}
	logger.Info("成功插入 Post", zap.Int("count", len(postsToCreate)))

	// --- 3. 准备并插入 PostDetail 数据 ---
	var detailsToCreate []*entities.PostDetail
	logger.Info("开始准备 PostDetail 数据...")
	processedPostCount := 0
	for _, post := range postsToCreate {
		if post.ID == 0 {
			logger.Warn("发现 Post ID 为 0 (GORM 未回填?)，无法为其创建 Detail", zap.Any("postTitle", post.Title))
			continue
		}

		// --- !!! 修正生成 Content 的逻辑 !!! ---
		numParagraphs := rand.Intn(4) + 2 // 随机生成 2 到 5 之间的数字
		var contentBuilder strings.Builder
		for p := 0; p < numParagraphs; p++ {
			contentBuilder.WriteString(faker.Paragraph()) // 调用 faker.Paragraph() 生成一段
			if p < numParagraphs-1 {
				contentBuilder.WriteString("\n\n") // 段落之间加两个换行符，模拟格式
			}
		}
		// --- Content 生成逻辑结束 ---

		detail := &entities.PostDetail{
			PostID:         post.ID,
			Content:        contentBuilder.String(), // 使用拼接好的内容
			PricePerUnit:   float64(rand.Intn(20000)+100) / 100.0,
			ContactQRCode:  faker.URL(),
			AuthorID:       post.AuthorID,
			AuthorAvatar:   post.AuthorAvatar,
			AuthorUsername: post.AuthorUsername,
			// 不要手动设置 CreatedAt 和 UpdatedAt
		}
		detailsToCreate = append(detailsToCreate, detail)
		processedPostCount++
		if processedPostCount%500 == 0 {
			logger.Info("已准备 PostDetail 数据...", zap.Int("count", processedPostCount), zap.Int("totalExpected", len(postsToCreate)))
		}
	}

	// --- 4. 批量插入 PostDetail 数据 ---
	if len(detailsToCreate) > 0 {
		logger.Info("开始批量插入 PostDetail...", zap.Int("batchSize", batchSize))
		if err := db.CreateInBatches(detailsToCreate, batchSize).Error; err != nil {
			logger.Error("批量插入 PostDetail 失败", zap.Error(err))
			return fmt.Errorf("批量插入 PostDetail 失败: %w", err)
		}
		logger.Info("成功插入 PostDetail", zap.Int("count", len(detailsToCreate)))
	} else {
		logger.Warn("没有生成任何 PostDetail 数据进行插入")
	}

	duration := time.Since(startTime)
	logger.Info("数据库填充完成！", zap.Duration("耗时", duration))
	return nil
}

// 注意：你可能需要在这个文件顶部添加 import "fmt" 和 "math/rand"

// ( loadDevConfig 函数可以去掉，因为 main.go 会加载配置并传入 db 和 logger )
