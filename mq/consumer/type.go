package consumer

import "github.com/Xushengqwer/post_service/models/enums"

// AuditResultMessage 代表从 Kafka 消费的审核结果消息
type AuditResultMessage struct {
	PostID    uint64      `json:"post_id"`          // 帖子 ID (确保是 uint64)
	NewStatus enums.Stats `json:"new_status"`       // 审核后的新状态 (使用 enums.Stats)
	Reason    string      `json:"reason,omitempty"` // 拒绝原因等（可选）
	// 可以添加其他必要字段，如操作时间等
}
