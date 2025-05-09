package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xushengqwer/post_service/models/dto"
	"time"

	"github.com/Xushengqwer/go-common/commonerrors" // 假设用于 ErrNotFound 等
	"github.com/Xushengqwer/go-common/core"
	"github.com/segmentio/kafka-go" // 引入 kafka-go 包
	"go.uber.org/zap"

	"github.com/Xushengqwer/post_service/service" // 引入服务层接口
)

// MessageHandler 定义了处理 Kafka 消息的接口 (可选, 但推荐)
type MessageHandler interface {
	Handle(ctx context.Context, msg kafka.Message) error
}

// AuditResultHandler 实现了 MessageHandler 接口，用于处理帖子审核结果
type AuditResultHandler struct {
	logger           *core.ZapLogger          // 日志记录器
	postAdminService service.PostAdminService // 依赖帖子管理员服务
}

// NewAuditResultHandler 创建 AuditResultHandler 实例
func NewAuditResultHandler(logger *core.ZapLogger, postAdminService service.PostAdminService) *AuditResultHandler {
	return &AuditResultHandler{
		logger:           logger,
		postAdminService: postAdminService,
	}
}

// Handle 处理单个 Kafka 消息
func (h *AuditResultHandler) Handle(ctx context.Context, msg kafka.Message) error {
	h.logger.Debug("开始处理 Kafka 消息",
		zap.String("topic", msg.Topic),
		zap.Int64("offset", msg.Offset),
		zap.Int("partition", msg.Partition),
		zap.ByteString("key", msg.Key))

	// 1. 反序列化消息体
	var auditMsg AuditResultMessage
	if err := json.Unmarshal(msg.Value, &auditMsg); err != nil {
		h.logger.Error("反序列化 Kafka 消息失败",
			zap.Error(err),
			zap.ByteString("message_value", msg.Value))
		// 反序列化失败通常意味着消息格式错误，重试可能无效，可以选择记录后丢弃
		// 或者发送到死信队列 (DLQ)，这里我们先记录日志并返回 nil 表示不再重试此消息
		return nil // 返回 nil，让 consumer 提交 offset，不再处理此错误消息
	}

	// 2. (可选) 基础验证
	//    可以根据需要添加对 auditMsg 字段的验证，例如状态值是否在预期范围内
	//    if auditMsg.NewStatus < enums.Pending || auditMsg.NewStatus > enums.Rejected { ... }
	h.logger.Info("成功解析审核结果消息",
		zap.Uint64("post_id", auditMsg.PostID),
		zap.Any("new_status", auditMsg.NewStatus),
		zap.String("reason", auditMsg.Reason)) // 记录 reason

	// 2. 构造调用服务层所需的 DTO
	//    创建一个 dto.AuditPostRequest 实例，填充从 Kafka 消息获得的数据
	auditRequest := &dto.AuditPostRequest{ // 注意是 dto.AuditPostRequest
		PostID: auditMsg.PostID,
		Status: auditMsg.NewStatus,
		Reason: auditMsg.Reason, // 直接将解析出的 Reason 赋给 DTO 的 Reason
	}

	// 3. 调用服务层更新帖子状态
	//    我们复用 PostAdminService 的 UpdatePostStatus 方法
	//    这里使用 context.Background() 或带超时的 context，因为处理可能独立于特定请求
	updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // 设置 5 秒超时
	defer cancel()

	err := h.postAdminService.AuditPost(updateCtx, auditRequest)
	if err != nil {
		// 处理更新失败的情况
		h.logger.Error("更新帖子状态失败",
			zap.Error(err),
			zap.Uint64("post_id", auditMsg.PostID),
			zap.Any("new_status", auditMsg.NewStatus),
			zap.String("reason", auditMsg.Reason))

		// 判断错误类型，决定是否需要重试
		// 例如，如果错误是 ErrRepoNotFound，可能不需要重试，因为帖子可能已被删除
		if errors.Is(err, commonerrors.ErrRepoNotFound) { // 假设 commonerrors 包含 ErrRepoNotFound
			h.logger.Warn("尝试更新不存在或已删除的帖子状态", zap.Uint64("post_id", auditMsg.PostID))
			return nil // 返回 nil，不再重试
		}

		// 对于其他类型的错误（如数据库暂时不可用），返回错误以便 Kafka 库进行重试（如果配置了）
		// 注意：kafka-go 的 ReadMessage 本身不直接支持自动重试，需要在消费循环或外部实现
		// 如果返回 error，consumer 循环可以选择不 commit offset，下次可能重新读取到
		// 但要小心无限重试，这里暂时返回 error，上层消费循环需要处理
		return fmt.Errorf("调用 UpdatePostStatus 失败: %w", err)
	}

	h.logger.Info("成功更新帖子状态", zap.Uint64("post_id", auditMsg.PostID))

	// 4. 处理成功，返回 nil
	return nil
}
