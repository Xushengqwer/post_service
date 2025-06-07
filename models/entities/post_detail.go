package entities

import "github.com/Xushengqwer/go-common/models/entities"

// PostDetail 帖子详情实体
// - 使用场景: 表示帖子详情页的数据，存储帖子详细内容、单价、作者信息和联系方式
// - 表名: post_details (GORM 默认使用结构体名复数形式)
// - 关系: 与 Post 表一对一关系，通过 PostID 外键关联
type PostDetail struct {
	entities.BaseModel // 嵌入自定义的 BaseModel , 包含 ID, CreatedAt, UpdatedAt, DeletedAt，支持软删除

	// 帖子ID，外键，关联 Post 表
	// - 类型: uint，与 Post 表的 ID 类型一致（GORM 默认使用 uint 作为主键类型）
	// - GORM 标签:
	//   - type:bigint 指定数据库类型（MySQL 中 uint 对应 bigint）
	//   - unique 确保一对一关系（一个 Post 只能有一个 PostDetail）
	//   - not null 表示非空
	//   - foreignKey:PostID 指定外键字段名
	//   - constraint:OnDelete:CASCADE 设置级联删除（删除 Post 时自动删除对应的 PostDetail）
	PostID uint64 `gorm:"type:bigint;unique;not null;foreignKey:PostID;constraint:OnDelete:CASCADE"`

	// 内容，支持多行文本，存储为 TEXT 类型
	// - 类型: text，适合存储较长的帖子详情内容，支持换行符（\n）
	// - GORM 标签: type:text 指定数据库类型，not null 表示非空
	// - 设计意图:
	//   - 详情页展示多行内容，例如“全程通话（天猫/京东/拼多多平台覆盖）\n500+中腰部KOL资源深度合作”
	//   - 存储时保留换行符（\n），前端根据换行符渲染为多行
	//   - 内容可以加载到 Redis 中缓存，Redis 存储为字符串，保留换行符
	Content string `gorm:"type:text;not null"`

	// 单价，存储服务的单价（单位：元）
	// - 类型: float64，使用浮点数存储价格，支持小数
	// - GORM 标签: type:decimal(10,2) 指定数据库类型（小数，10位总长度，2位小数），default:0 设置默认值为 0
	// - 设计意图:
	//   - 详情页展示单价，例如 10 万元/单，存储为 100000.00（单位：元）
	//   - 前端展示时可直接显示为“10万元/单”
	PricePerUnit float64 `gorm:"type:decimal(10,2);default:0"`

	// 联系方式，主要用于填写并存储用户的手机号、微信号、QQ号等文本形式的联系信息。
	// - 类型: varchar(255)，适合存储上述文本联系方式，最大长度255字符。
	// - GORM 标签: `gorm:"type:varchar(255);not null"` 指定数据库字段类型为varchar(255)，并约束该字段不能为空。
	// - 设计意图: 在用户界面（如个人资料页）清晰展示这些核心联系方式，方便其他用户直接复制ID/号码、发起呼叫或添加好友等操作。
	ContactInfo string `gorm:"type:varchar(255);not null"`
}
