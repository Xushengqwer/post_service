package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Xushengqwer/go-common/commonerrors"
	"github.com/Xushengqwer/go-common/core"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Xushengqwer/go-common/models/enums"       // 假设 enums 在这里
	"github.com/Xushengqwer/go-common/models/kafkaevents" // 导入统一的事件结构

	"github.com/Xushengqwer/post_service/models/dto"
	"github.com/Xushengqwer/post_service/service"
)

// todo  未配置死信队列

// MessageHandler 定义了处理 Kafka 消息的接口 (保持不变)
type MessageHandler interface {
	Handle(ctx context.Context, msg kafka.Message) error
}

// --- ApprovedAuditHandler ---

type ApprovedAuditHandler struct {
	logger           *core.ZapLogger
	postAdminService service.PostAdminService
}

func NewApprovedAuditHandler(logger *core.ZapLogger, postAdminService service.PostAdminService) *ApprovedAuditHandler {
	return &ApprovedAuditHandler{
		logger:           logger,
		postAdminService: postAdminService,
	}
}

func (h *ApprovedAuditHandler) Handle(ctx context.Context, msg kafka.Message) error {
	h.logger.Debug("ApprovedAuditHandler: 开始处理 Kafka 消息", zap.String("topic", msg.Topic))

	// 2. 使用从 common 包导入的 kafkaevents.PostApprovedEvent
	var event kafkaevents.PostApprovedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		h.logger.Error("ApprovedAuditHandler: 反序列化 Kafka 消息失败", zap.Error(err), zap.ByteString("value", msg.Value))
		return nil // 不重试无法解析的消息
	}

	// 从事件中获取 PostID (注意：我们统一的 PostData 结构中字段是 ID)
	postID := event.Post.ID
	h.logger.Info("ApprovedAuditHandler: 成功解析审核通过消息",
		zap.String("event_id", event.EventID),
		zap.Uint64("post_id", postID))

	auditRequest := &dto.AuditPostRequest{
		PostID: postID,
		Status: enums.Approved, // 使用 common/enums 中的 Approved
		Reason: "",
	}

	updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.postAdminService.AuditPost(updateCtx, auditRequest)
	if err != nil {
		h.logger.Error("ApprovedAuditHandler: 更新帖子状态为已通过失败", zap.Error(err), zap.Uint64("post_id", postID))
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			h.logger.Warn("ApprovedAuditHandler: 尝试更新不存在或已删除的帖子状态", zap.Uint64("post_id", postID))
			return nil // 不再重试
		}
		return fmt.Errorf("ApprovedAuditHandler: 调用 AuditPost 失败: %w", err)
	}

	h.logger.Info("ApprovedAuditHandler: 成功更新帖子状态为已通过", zap.Uint64("post_id", postID))
	return nil
}

// --- RejectedAuditHandler ---

type RejectedAuditHandler struct {
	logger           *core.ZapLogger
	postAdminService service.PostAdminService
}

func NewRejectedAuditHandler(logger *core.ZapLogger, postAdminService service.PostAdminService) *RejectedAuditHandler {
	return &RejectedAuditHandler{
		logger:           logger,
		postAdminService: postAdminService,
	}
}

// formatRejectionReason 拼接审核拒绝原因
// (现在使用 kafkaevents.RejectionDetail)
func (h *RejectedAuditHandler) formatRejectionReason(event *kafkaevents.PostRejectedEvent) string {
	var reasonBuilder strings.Builder

	reasonBuilder.WriteString(fmt.Sprintf("Suggestion: %s.", event.Suggestion))

	if len(event.Details) > 0 {
		reasonBuilder.WriteString(" Details: [")
		var detailStrings []string
		for _, detail := range event.Details {
			// 使用 kafkaevents.RejectionDetail 的字段
			matched := ""
			if len(detail.MatchedContent) > 0 {
				matched = fmt.Sprintf(", Matched: '%s'", strings.Join(detail.MatchedContent, "','"))
			}
			detailStrings = append(detailStrings,
				fmt.Sprintf("{Label: %s, Suggestion: %s, Score: %.2f%s}",
					detail.Label, detail.Suggestion, detail.Score, matched))
		}
		reasonBuilder.WriteString(strings.Join(detailStrings, "; "))
		reasonBuilder.WriteString("]")
	}

	reasonStr := reasonBuilder.String()
	const maxReasonLength = 250 // 假设数据库字段长度为 255
	if len(reasonStr) > maxReasonLength {
		reasonStr = reasonStr[:maxReasonLength] + "..."
	}
	return reasonStr
}

func (h *RejectedAuditHandler) Handle(ctx context.Context, msg kafka.Message) error {
	h.logger.Debug("RejectedAuditHandler: 开始处理 Kafka 消息", zap.String("topic", msg.Topic))

	// 3. 使用从 common 包导入的 kafkaevents.PostRejectedEvent
	var event kafkaevents.PostRejectedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		h.logger.Error("RejectedAuditHandler: 反序列化 Kafka 消息失败", zap.Error(err), zap.ByteString("value", msg.Value))
		return nil // 不重试无法解析的消息
	}

	postID := event.PostID
	auditReason := h.formatRejectionReason(&event)

	h.logger.Info("RejectedAuditHandler: 成功解析审核拒绝消息",
		zap.String("event_id", event.EventID),
		zap.Uint64("post_id", postID),
		zap.String("generated_reason", auditReason))

	auditRequest := &dto.AuditPostRequest{
		PostID: postID,
		Status: enums.Rejected, // 使用 common/enums 中的 Rejected
		Reason: auditReason,
	}

	updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := h.postAdminService.AuditPost(updateCtx, auditRequest)
	if err != nil {
		h.logger.Error("RejectedAuditHandler: 更新帖子状态为已拒绝失败",
			zap.Error(err),
			zap.Uint64("post_id", postID),
			zap.String("reason", auditReason))
		if errors.Is(err, commonerrors.ErrRepoNotFound) {
			h.logger.Warn("RejectedAuditHandler: 尝试更新不存在或已删除的帖子状态", zap.Uint64("post_id", postID))
			return nil // 不再重试
		}
		return fmt.Errorf("RejectedAuditHandler: 调用 AuditPost 失败: %w", err)
	}

	h.logger.Info("RejectedAuditHandler: 成功更新帖子状态为已拒绝", zap.Uint64("post_id", postID))
	return nil
}
