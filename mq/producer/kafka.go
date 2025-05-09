package producer

import (
	"context"
	"encoding/json"
	"github.com/Xushengqwer/go-common/core"
	"github.com/Xushengqwer/post_service/config"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// KafkaProducer Kafka 消息生产者
type KafkaProducer struct {
	writer *kafka.Writer
	logger *core.ZapLogger
	topics config.Topics // 存储主题配置，便于访问
}

// NewKafkaProducer 创建一个新的 Kafka 生产者实例
// - 意图: 初始化 Kafka 生产者，用于发送消息到 Kafka
// - 输入: config config.KafkaConfig 配置信息, logger *core.ZapLogger 日志记录器
// - 输出: *KafkaProducer 生产者实例
// - 注意事项: 不绑定具体主题，发送时动态指定
func NewKafkaProducer(config config.KafkaConfig, logger *core.ZapLogger) *KafkaProducer {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(config.Brokers...),
		Balancer: &kafka.LeastBytes{},
		// Topic 字段留空，动态指定
	}
	return &KafkaProducer{
		writer: writer,
		logger: logger,
		topics: config.Topics, // 保存主题配置
	}
}

// SendEvent 发送事件到指定 Kafka 主题
// - 意图: 将事件发送到指定的 Kafka 主题
// - 输入: ctx context.Context 上下文, topic string 主题, event interface{} 事件数据
// - 输出: error 错误信息
// - 注意事项: 事件数据会被 JSON 序列化
func (p *KafkaProducer) SendEvent(ctx context.Context, topic string, event interface{}) error {
	eventBytes, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("Failed to marshal event", zap.Error(err), zap.String("topic", topic))
		return err
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: eventBytes,
	})
}

// SendPostAuditEvent 发送帖子审核事件到 Kafka
// - 意图: 将帖子事件发送到 Kafka 主题，供下游服务（如审核服务）消费
// - 输入: ctx context.Context 上下文, event PostEvent 帖子事件结构体
// - 输出: error 错误信息
// - 注意事项: 事件包含帖子完整信息，确保下游服务无需额外查询数据库
func (p *KafkaProducer) SendPostAuditEvent(ctx context.Context, event PostEvent) error {
	return p.SendEvent(ctx, p.topics.PostAuditRequest, event)
}

// SendPostDeleteEvent 发送帖子删除事件到 Kafka
// - 意图: 将帖子删除事件发送到 PostDelete 主题
// - 输入: ctx context.Context 上下文, postID uint64 帖子ID
// - 输出: error 错误信息
// - 注意事项: 事件仅包含帖子ID，供 ES 服务删除数据
func (p *KafkaProducer) SendPostDeleteEvent(ctx context.Context, postID uint64) error {
	event := map[string]interface{}{
		"operation": "delete",
		"post_id":   postID,
	}
	return p.SendEvent(ctx, p.topics.PostDelete, event)
}
