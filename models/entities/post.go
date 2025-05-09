package entities

import (
	"database/sql"
	"github.com/Xushengqwer/go-common/models/entities"
	"github.com/Xushengqwer/go-common/models/enums"
)

// Post 帖子简略实体
// - 使用场景: 表示帖子列表页的数据，存储标题、作者信息、状态、浏览量、官方标签等
// - 表名: posts (GORM 默认使用结构体名复数形式)
type Post struct {
	entities.BaseModel // 嵌入自定义的 BaseModel ,包含 ID, CreatedAt, UpdatedAt, DeletedAt，支持软删除

	// 标题，必填，最大长度255个字符
	// - 类型: varchar(255)，限制长度以提高查询效率，适合帖子标题的长度需求
	// - GORM 标签: type:varchar(255) 指定数据库类型，not null 表示非空
	Title string `gorm:"type:varchar(255);not null"`

	// 作者ID，关联用户表，外键
	// - 类型: char(36)，用户ID为UUID格式（36个字符）
	// - GORM 标签: type:char(36) 指定固定长度字符，not null 表示非空
	AuthorID string `gorm:"type:char(36);not null"`

	// 作者头像，存储作者头像的URL或路径
	// - 类型: varchar(255)，限制长度为 255 个字符，适合存储URL（例如“https://example.com/avatar.jpg”）
	// - GORM 标签: type:varchar(255) 指定数据库类型，not null 表示非空
	// - 设计意图: 列表页直接展示作者头像，避免跨服务调用
	// - 注意:
	//   - 该字段为冗余字段，数据来源于用户服务，更新时通过异步消息队列同步
	//   - 用户注册时会有默认头像，因此字段不可为空
	AuthorAvatar string `gorm:"type:varchar(255);not null"`

	// 作者用户名，存储作者的用户名
	// - 类型: varchar(50)，限制长度为 50 个字符，适合存储用户名（例如“云创科技”）
	// - GORM 标签: type:varchar(50) 指定数据库类型，not null 表示非空
	// - 设计意图: 列表页直接展示作者用户名，避免跨服务调用
	// - 注意: 该字段为冗余字段，数据来源于用户服务，更新时通过异步消息队列同步
	AuthorUsername string `gorm:"type:varchar(50);not null"`

	// 状态，枚举类型：0=待审核, 1=已审核, 2=拒绝
	// - 类型: int，使用整数表示枚举值，便于扩展和查询
	// - GORM 标签: type:int 指定整数类型，default:0 设置默认值为待审核
	Status enums.Status `gorm:"type:int;default:0"`

	// 浏览量，统计帖子的浏览次数
	// - 类型: int64，记录浏览次数，默认值为0
	// - GORM 标签: type:int 指定整数类型，default:0 设置默认值
	ViewCount int64 `gorm:"type:int;default:0"`

	// 官方标签，标识帖子的官方认证状态
	// - 类型: int，使用枚举值表示官方标签（参考 enums.OfficialTag）
	// - GORM 标签: type:int 指定整数类型，default:0 设置默认值为无标签
	OfficialTag enums.OfficialTag `gorm:"type:int;default:0"`

	// 审核原因，记录帖子审核（特别是拒绝时）的原因
	// - 类型: sql.NullString，可以为 NULL 的字符串，用于存储可能不存在的原因
	// - GORM 标签: type:varchar(255) 指定数据库类型；comment:审核原因 添加数据库列注释
	AuditReason sql.NullString `gorm:"type:varchar(255);comment:审核原因"`
}
