package consumer

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	appConfig "github.com/Xushengqwer/post_service/config"
	// 不再需要 "github.com/Xushengqwer/post_service/constant"
)

// Consumer 定义 Kafka 消费者结构 (保持不变)
type Consumer struct {
	reader  *kafka.Reader
	handler MessageHandler
	logger  *core.ZapLogger
	topic   string
}

// NewConsumer 创建 Kafka Consumer 实例 (修改为直接接收 topicName)
func NewConsumer(cfg *appConfig.KafkaConfig, groupID string, topicName string, handler MessageHandler, logger *core.ZapLogger) (*Consumer, error) {

	// 检查 topicName 是否为空
	if topicName == "" {
		return nil, errors.New("kafka topic 名称不能为空")
	}
	// 检查 Brokers 是否为空
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 配置不能为空")
	}

	logger.Info("初始化 Kafka 消费者",
		zap.Strings("brokers", cfg.Brokers),
		zap.String("topic", topicName),
		zap.String("group_id", groupID))

	// 使用 segmentio/kafka-go 的 NewReader
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          topicName, // <--- 直接使用传入的 topicName
		GroupID:        groupID,
		MinBytes:       10e3,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		MaxWait:        3 * time.Second,
	})

	return &Consumer{
		reader:  reader,
		handler: handler,
		logger:  logger,
		topic:   topicName,
	}, nil
}

// Start 启动消费者循环来读取和处理消息 (保持不变)
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("Kafka 消费者已启动", zap.String("topic", c.topic))
	defer c.logger.Info("Kafka 消费者已停止", zap.String("topic", c.topic))

	for {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			c.logger.Warn("消费者上下文已取消，正在退出...", zap.String("topic", c.topic))
			return
		default:
			// 继续执行
		}

		msg, err := c.reader.ReadMessage(ctx)

		if err != nil {
			// 如果 context 被取消或 Reader 关闭，正常退出
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				c.logger.Warn("消费者读取循环退出", zap.String("topic", c.topic), zap.Error(err))
				return
			}
			c.logger.Error("读取 Kafka 消息失败", zap.String("topic", c.topic), zap.Error(err))
			time.Sleep(1 * time.Second) // 简单等待后重试
			continue
		}

		handleCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		handleErr := c.handler.Handle(handleCtx, msg)
		cancel()

		if handleErr != nil {
			c.logger.Error("处理 Kafka 消息时发生错误",
				zap.Error(handleErr),
				zap.String("topic", msg.Topic),
				zap.Int64("offset", msg.Offset))
		}
	}
}

// Close 关闭 Kafka Reader (保持不变)
func (c *Consumer) Close() error {
	c.logger.Info("正在关闭 Kafka 消费者...", zap.String("topic", c.topic))
	if err := c.reader.Close(); err != nil {
		c.logger.Error("关闭 Kafka Reader 失败", zap.Error(err), zap.String("topic", c.topic))
		return err
	}
	c.logger.Info("Kafka 消费者已成功关闭", zap.String("topic", c.topic))
	return nil
}
