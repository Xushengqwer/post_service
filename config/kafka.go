package config

type KafkaConfig struct {
	Brokers         []string `mapstructure:"brokers" json:"brokers" yaml:"brokers"`
	Topics          Topics   `mapstructure:"topics" json:"topics" yaml:"topics"`
	ConsumerGroupID string   `mapstructure:"consumer_group_id" json:"consumer_group_id" yaml:"consumer_group_id"`
}

type Topics struct {
	PostAuditRequest string `mapstructure:"postAuditRequest" json:"postAuditRequest" yaml:"postAuditRequest"`
	PostAuditResult  string `mapstructure:"postAuditResult" json:"postAuditResult" yaml:"postAuditResult"`
	PostDelete       string `mapstructure:"postDelete" json:"postDelete" yaml:"postDelete"`
}
