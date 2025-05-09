package constant

import (
	"time"
)

// Redis Key 相关常量 (导出)
const (
	// Key 前缀或模式 (统一管理，便于修改)
	PostViewBloomPrefix       = "post_view_bloom:"              //<--- 改为大写导出
	PostViewCountPrefix       = "post_view_count:"              //<--- 改为大写导出
	PostDetailCacheKeyPrefix  = "post_detail:"                  //<--- 改为大写导出
	FailedDetailSerializeList = "failed_post_details_serialize" //<--- 改为大写导出

	// 固定 Key 名称
	PostsRankKey    = "post_rank"     //<--- 改为大写导出
	HotPostsRankKey = "hot_post_rank" //<--- 改为大写导出
	PostsHashKey    = "posts"         //<--- 改为大写导出

	// SCAN 模式
	PostDetailKeyPattern = PostDetailCacheKeyPrefix + "*" //<--- 改为大写导出 (依赖 PostDetailCacheKeyPrefix)
	PostViewCountPattern = PostViewCountPrefix + "*"      //<--- 改为大写导出 (依赖 PostViewCountPrefix)
)

// Redis TTL 相关常量 (已导出)
const (
	ViewTTL       time.Duration = 12 * time.Hour
	PostDetailTTL time.Duration = 24 * time.Hour
)
