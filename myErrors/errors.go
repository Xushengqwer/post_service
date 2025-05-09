package myErrors

import "errors"

// ErrCacheMiss 表示在缓存层未找到对应的键值
var ErrCacheMiss = errors.New("cache: key not found (miss)")
