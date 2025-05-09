package consumer

import (
	"context"
	"errors"
	"fmt"
	"io" // 需要导入 io 包
	"os"
	"time"

	"github.com/Xushengqwer/go-common/core"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	appConfig "github.com/Xushengqwer/post_service/config" // 引入应用配置
)

// Consumer 定义 Kafka 消费者结构
type Consumer struct {
	reader  *kafka.Reader
	handler MessageHandler
	logger  *core.ZapLogger
	topic   string
}

// NewConsumer 创建 Kafka Consumer 实例
func NewConsumer(cfg *appConfig.KafkaConfig, groupID string, topicKey string, handler MessageHandler, logger *core.ZapLogger) (*Consumer, error) {
	// 从配置中获取具体的主题名称
	var topicName string
	switch topicKey {
	case "PostAuditResult":
		topicName = cfg.Topics.PostAuditResult
	// 可以添加其他 case 来处理不同的主题
	default:
		return nil, fmt.Errorf("未知的主题键: %s", topicKey)
	}

	if topicName == "" {
		return nil, fmt.Errorf("主题 '%s' 在配置中未定义或为空", topicKey)
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 配置不能为空")
	}

	logger.Info("初始化 Kafka 消费者",
		zap.Strings("brokers", cfg.Brokers),
		zap.String("topic", topicName),
		zap.String("group_id", groupID))

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   topicName,
		GroupID: groupID,
		// Partition:    0, // 通常不指定特定分区，让 Group 自动平衡
		MinBytes:       10e3,            // 10KB，减少小的网络请求
		MaxBytes:       10e6,            // 10MB，根据消息大小调整
		CommitInterval: time.Second,     // 每秒自动提交一次 offset（如果使用 ReadMessage）
		MaxWait:        3 * time.Second, // 等待新消息的最大时间
		// ReadLagInterval: -1, // 禁用自动 Lag 上报（如果不需要）
		// GroupBalancers: []kafka.GroupBalancer{kafka.RangeGroupBalancer{}}, // 选择平衡策略
		// HeartbeatInterval: 3 * time.Second, // 根据需要调整
		// SessionTimeout:    30 * time.Second, // 根据需要调整
		// StartOffset:       kafka.FirstOffset, // 或 LastOffset
	})

	return &Consumer{
		reader:  reader,
		handler: handler,
		logger:  logger,
		topic:   topicName,
	}, nil
}

// Start 启动消费者循环来读取和处理消息
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("Kafka 消费者已启动", zap.String("topic", c.topic))
	defer c.logger.Info("Kafka 消费者已停止", zap.String("topic", c.topic))

	for {
		// 使用 ReadMessage 会自动（或根据 CommitInterval）提交 offset
		// 如果 Handle 返回 error，ReadMessage 不会提交当前消息的 offset，
		// 下次读取可能会再次读到，需要小心处理潜在的无限重试。
		// FetchMessage 提供更手动的 offset 提交控制。
		msg, err := c.reader.ReadMessage(ctx)

		// 检查错误类型
		if err != nil {
			// 如果是 context 被取消，则正常退出
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				c.logger.Warn("消费者读取循环退出", zap.String("topic", c.topic), zap.Error(err))
				return // 退出循环
			}
			// 其他类型的读取错误
			c.logger.Error("读取 Kafka 消息失败", zap.String("topic", c.topic), zap.Error(err))
			// 可以根据错误类型决定是否继续循环或退出
			// 例如，如果是临时网络问题，可以短暂 sleep 后 continue
			// 如果是配置错误，可能需要退出
			time.Sleep(1 * time.Second) // 简单处理：等待后继续尝试
			continue
		}

		// 处理消息
		// 为每个消息处理创建一个独立的 context，可能带有超时
		handleCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // 例如，设置 30 秒处理超时
		handleErr := c.handler.Handle(handleCtx, msg)
		cancel() // 及时释放资源

		if handleErr != nil {
			c.logger.Error("处理 Kafka 消息时发生错误",
				zap.Error(handleErr),
				zap.String("topic", msg.Topic),
				zap.Int64("offset", msg.Offset))
			// 处理失败，决定如何操作：
			// 1. 记录日志，offset 已被 ReadMessage 自动提交（默认行为），消息丢失。
			// 2. 如果希望重试，之前的 Handle 就不该返回 error，或者这里需要特殊逻辑
			//    来阻止 offset 提交（例如使用 FetchMessage）或将消息发送到 DLQ。
			// 当前实现下，错误已被记录，循环继续处理下一条。
		}
		// ReadMessage 会在处理完成后（无论成功与否）自动提交 offset (基于 CommitInterval)
	}
}

// Close 关闭 Kafka Reader
func (c *Consumer) Close() error {
	c.logger.Info("正在关闭 Kafka 消费者...", zap.String("topic", c.topic))
	if err := c.reader.Close(); err != nil {
		c.logger.Error("关闭 Kafka Reader 失败", zap.Error(err), zap.String("topic", c.topic))
		return err
	}
	c.logger.Info("Kafka 消费者已成功关闭", zap.String("topic", c.topic))
	return nil
}
