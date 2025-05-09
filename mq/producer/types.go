package producer

import "github.com/Xushengqwer/post_service/models/enums"

// PostEvent 表示 Kafka 传递的帖子事件结构体
type PostEvent struct {
	ID             uint64            `json:"id"`              // 帖子ID
	Title          string            `json:"title"`           // 帖子标题
	Content        string            `json:"content"`         // 帖子内容
	AuthorID       string            `json:"author_id"`       // 作者ID
	AuthorAvatar   string            `json:"author_avatar"`   // 作者头像
	AuthorUsername string            `json:"author_username"` // 作者用户名
	Status         enums.Stats       `json:"status"`          // 帖子状态
	ViewCount      int64             `json:"view_count"`      // 查看次数
	OfficialTag    enums.OfficialTag `json:"official_tag"`    // 官方标签
	PricePerUnit   float64           `json:"price_per_unit"`  // 每单位价格
	ContactQRCode  string            `json:"contact_qr_code"` // 联系二维码
}
