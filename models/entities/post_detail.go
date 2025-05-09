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

	// 作者ID，存储帖子的作者ID
	// - 类型: char(36)，与 Post 表的 AuthorID 一致，用户ID为UUID格式（36个字符）
	// - GORM 标签: type:char(36) 指定固定长度字符，not null 表示非空
	// - 设计意图: 详情页直接展示作者信息，避免跨服务调用
	// - 注意: 该字段为冗余字段，与 Post 表的 AuthorID 一致，更新时通过异步消息队列同步
	AuthorID string `gorm:"type:char(36);not null"`

	// 作者头像，存储作者头像的URL或路径
	// - 类型: varchar(255)，限制长度为 255 个字符，适合存储URL（例如“https://example.com/avatar.jpg”）
	// - GORM 标签: type:varchar(255) 指定数据库类型，not null 表示非空
	// - 设计意图: 详情页直接展示作者头像，避免跨服务调用
	// - 注意:
	//   - 该字段为冗余字段，数据来源于用户服务，更新时通过异步消息队列同步
	//   - 用户注册时会有默认头像，因此字段不可为空
	AuthorAvatar string `gorm:"type:varchar(255);not null"`

	// 作者用户名，存储作者的用户名
	// - 类型: varchar(50)，限制长度为 50 个字符，适合存储用户名（例如“云创科技”）
	// - GORM 标签: type:varchar(50) 指定数据库类型，not null 表示非空
	// - 设计意图: 详情页直接展示作者用户名，避免跨服务调用
	// - 注意: 该字段为冗余字段，数据来源于用户服务，更新时通过异步消息队列同步
	AuthorUsername string `gorm:"type:varchar(50);not null"`

	// 联系方式二维码，存储二维码图片的URL或路径
	// - 类型: varchar(255)，限制长度为 255 个字符，适合存储URL（例如“https://example.com/qrcode.jpg”）
	// - GORM 标签: type:varchar(255) 指定数据库类型，not null 表示非空
	// - 设计意图: 详情页展示联系方式二维码（例如“立即联系”按钮关联的二维码）
	ContactQRCode string `gorm:"type:varchar(255);not null"`
}
