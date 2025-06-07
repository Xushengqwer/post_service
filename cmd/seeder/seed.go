package main // <--- 确保这里是 package main

import (
	"context"
	"fmt"
	"sync" // 用于并发控制（如果需要）

	"github.com/Xushengqwer/go-common/core"
	"github.com/brianvoe/gofakeit/v6"
	"github.com/google/uuid" // 用于生成 AuthorID
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/service" // 引入 service 包
)

// Seed 函数现在接收 PostService 实例、logger 和要生成的帖子数量
// 注意：函数名 Seed 首字母大写，以便在同一个包中被 main.go 调用
func Seed(ctx context.Context, postSvc service.PostService, logger *core.ZapLogger, numPosts int) {
	logger.Info("开始填充测试数据 (通过服务层)...", zap.Int("数量", numPosts))

	var wg sync.WaitGroup
	concurrencyLimit := 10
	semaphore := make(chan struct{}, concurrencyLimit)

	for i := 0; i < numPosts; i++ {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(itemIndex int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			authorID := uuid.New().String()
			authorUsername := gofakeit.Username()
			authorAvatar := gofakeit.ImageURL(100, 100)

			createReq := &dto.CreatePostRequest{
				Title:          gofakeit.Sentence(gofakeit.Number(5, 15)),
				Content:        gofakeit.Paragraph(3, 5, 20, "\n\n"),
				AuthorID:       authorID,
				AuthorAvatar:   authorAvatar,
				AuthorUsername: authorUsername,
				PricePerUnit:   gofakeit.Price(10, 1000),
				ContactInfo:    gofakeit.ImageURL(200, 200),
			}

			resp, err := postSvc.CreatePost(ctx, createReq, nil)
			if err != nil {
				logger.Error(fmt.Sprintf("创建帖子 %d/%d 失败", itemIndex+1, numPosts),
					zap.Error(err),
					zap.String("title", createReq.Title),
					zap.String("author_id", createReq.AuthorID))
			} else {
				logger.Info(fmt.Sprintf("成功创建帖子 %d/%d", itemIndex+1, numPosts),
					zap.Uint64("post_id", resp.ID),
					zap.String("title", resp.Title))
			}
		}(i)
	}

	wg.Wait()
	logger.Info("测试数据填充完毕 (通过服务层)。")
}
