package config

import "github.com/Xushengqwer/go-common/config"

type PostConfig struct {
	ZapConfig      config.ZapConfig     `mapstructure:"zapConfig" json:"zapConfig" yaml:"zapConfig"`
	GormLogConfig  config.GormLogConfig `mapstructure:"gormLogConfig" json:"gormLogConfig" yaml:"gormLogConfig"`
	ServerConfig   config.ServerConfig  `mapstructure:"serverConfig" json:"serverConfig" yaml:"serverConfig"`
	TracerConfig   config.TracerConfig  `mapstructure:"tracerConfig" json:"tracerConfig" yaml:"tracerConfig"`
	ViewSyncConfig ViewSyncConfig       `mapstructure:"viewSyncConfig" json:"viewSyncConfig" yaml:"viewSyncConfig"`
	MySQLConfig    MySQLConfig          `mapstructure:"mysqlConfig" json:"mysqlConfig" yaml:"mysqlConfig"`
	RedisConfig    RedisConfig          `mapstructure:"redisConfig" json:"redisConfig" yaml:"redisConfig"`
	KafkaConfig    KafkaConfig          `mapstructure:"kafkaConfig" json:"kafkaConfig" yaml:"kafkaConfig"`
	COSConfig      COSConfig            `mapstructure:"postDetailImagesCosConfig" json:"postDetailImagesCosConfig" yaml:"postDetailImagesCosConfig"`
}
