package config

type KafkaConfig struct {
	Brokers         []string `mapstructure:"brokers" json:"brokers" yaml:"brokers"`
	Topics          Topics   `mapstructure:"topics" json:"topics" yaml:"topics"`
	ConsumerGroupID string   `mapstructure:"consumer_group_id" json:"consumer_group_id" yaml:"consumer_group_id"`
}

type Topics struct {
	PostPendingAudit  string `mapstructure:"postPendingAudit" yaml:"postPendingAudit"`   //  提交审核主题
	PostAuditApproved string `mapstructure:"postAuditApproved" yaml:"postAuditApproved"` //  审核通过主题
	PostAuditRejected string `mapstructure:"postAuditRejected" yaml:"postAuditRejected"` //  审核拒绝主题
	PostDeleted       string `mapstructure:"postDeleted" yaml:"postDeleted"`             //  帖子删除主题
}
