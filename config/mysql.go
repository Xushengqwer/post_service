package config

// SourceConfig 代表一个数据库源（主库或从库）的配置
type SourceConfig struct {
	DSN string `mapstructure:"dsn" yaml:"dsn"` // 直接使用 DSN 字符串
	// 保留独立的连接池设置，允许覆盖共享设置 (可选)
	MaxIdleConns    *int `mapstructure:"max_idle_conns,omitempty" yaml:"max_idle_conns,omitempty"`       // 使用指针以区分是否设置
	MaxOpenConns    *int `mapstructure:"max_open_conns,omitempty" yaml:"max_open_conns,omitempty"`       // 使用指针以区分是否设置
	ConnMaxLifetime *int `mapstructure:"conn_max_lifetime,omitempty" yaml:"conn_max_lifetime,omitempty"` // 使用指针以区分是否设置 (秒)
}

// MySQLConfig 包含主库和从库的配置 (使用 DSN)
type MySQLConfig struct {
	Write SourceConfig   `mapstructure:"write" yaml:"write"` // 主库配置
	Read  []SourceConfig `mapstructure:"read" yaml:"read"`   // 从库配置列表 (可以为空，表示不启用读写分离)

	// 共享/默认连接池设置 (如果 Write/Read 中未指定，则使用这些值)
	SharedMaxIdleConns    int `mapstructure:"max_idle_conns" yaml:"max_idle_conn"`        // 共享/默认设置
	SharedMaxOpenConns    int `mapstructure:"max_open_conn" yaml:"max_open_conn"`         // 共享/默认设置，确保足够大
	SharedConnMaxLifetime int `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"` // 共享/默认设置（秒）
}
