package producer

import (
	"context"
	"encoding/json"
	"time" // 引入 time 包

	"github.com/Xushengqwer/go-common/core"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/Xushengqwer/go-common/models/kafkaevents"
	"github.com/Xushengqwer/post_service/config"
)

// KafkaProducer Kafka 消息生产者 (保持不变)
type KafkaProducer struct {
	writer *kafka.Writer
	logger *core.ZapLogger
	topics config.Topics
}

// NewKafkaProducer 创建一个新的 Kafka 生产者实例 (保持不变)
func NewKafkaProducer(config config.KafkaConfig, logger *core.ZapLogger) *KafkaProducer {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(config.Brokers...),
		Balancer: &kafka.LeastBytes{},
	}
	return &KafkaProducer{
		writer: writer,
		logger: logger,
		topics: config.Topics,
	}
}

// SendEvent 发送事件到指定 Kafka 主题 (保持不变，但现在会处理统一的事件结构)
func (p *KafkaProducer) SendEvent(ctx context.Context, topic string, event interface{}) error {
	eventBytes, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("Failed to marshal event", zap.Error(err), zap.String("topic", topic))
		return err
	}

	p.logger.Debug("Sending Kafka message",
		zap.String("topic", topic),
		zap.ByteString("payload", eventBytes))

	err = p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: eventBytes,
	})

	if err != nil {
		p.logger.Error("Failed to write Kafka message", zap.Error(err), zap.String("topic", topic))
	} else {
		p.logger.Info("Successfully sent Kafka message", zap.String("topic", topic))
	}
	return err
}

// SendPostPendingAuditEvent 发送帖子待审核事件到 Kafka (重构)
// - 意图: 将新创建或更新的帖子发送到 Kafka 供审核服务消费
// - 输入: ctx context.Context 上下文, postData kafkaevents.PostData 帖子核心数据
// - 输出: error 错误信息
func (p *KafkaProducer) SendPostPendingAuditEvent(ctx context.Context, postData kafkaevents.PostData) error {
	// 1. 创建统一的 PostPendingAuditEvent 事件
	event := kafkaevents.PostPendingAuditEvent{
		EventID:   uuid.New().String(), // 生成唯一的 EventID
		Timestamp: time.Now(),          // 设置当前时间戳
		Post:      postData,            // 嵌入 PostData
	}

	// 2. 发送事件到 PostPendingAudit 主题
	//    注意：我们现在从 p.topics.PostPendingAudit 获取主题名称
	return p.SendEvent(ctx, p.topics.PostPendingAudit, event)
}

// SendPostDeleteEvent 发送帖子删除事件到 Kafka (重构)
// - 意图: 将帖子删除事件发送到 PostDeleted 主题
// - 输入: ctx context.Context 上下文, postID uint64 帖子ID
// - 输出: error 错误信息
func (p *KafkaProducer) SendPostDeleteEvent(ctx context.Context, postID uint64) error {
	// 1. 创建统一的 PostDeletedEvent 事件
	event := kafkaevents.PostDeletedEvent{
		EventID:   uuid.New().String(), // 生成唯一的 EventID
		Timestamp: time.Now(),          // 设置当前时间戳
		PostID:    postID,              // 设置 PostID
	}

	// 2. 发送事件到 PostDeleted 主题
	//    注意：我们现在从 p.topics.PostDeleted 获取主题名称
	return p.SendEvent(ctx, p.topics.PostDeleted, event)
}
