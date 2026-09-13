package breacloud

import (
	"context"
	"sync"
	"time"
)

// DefaultCacheTTL 是服务列表的默认缓存时长。
const DefaultCacheTTL = 5 * time.Minute

// ServiceCache 是带 TTL 的服务列表缓存。
//
// 动机：一次日报会多次读取服务列表（渲染、聚合、消息里查名字），而大账号下
// 每次刷新都要翻很多页。Bot 的 /vps 与定时任务共用同一个实例。
type ServiceCache struct {
	client *Client
	ttl    time.Duration

	mu    sync.Mutex
	items []Service
	at    time.Time
	now   func() time.Time
}

// NewServiceCache 构造缓存。ttl <= 0 时使用 DefaultCacheTTL。
func NewServiceCache(client *Client, ttl time.Duration) *ServiceCache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &ServiceCache{client: client, ttl: ttl, now: time.Now}
}

// List 返回服务列表。force 为真或缓存过期时重新拉取。
//
// 返回的是副本，调用方随便改都不会污染缓存。
func (s *ServiceCache) List(ctx context.Context, force bool) ([]Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !force && s.items != nil && s.now().Sub(s.at) < s.ttl {
		return copyServices(s.items), nil
	}
	items, err := s.client.ListServices(ctx)
	if err != nil {
		// 拉取失败时保留旧数据：过期列表比没有列表好，剩下的由调用方决定怎么提示。
		if s.items != nil {
			return copyServices(s.items), err
		}
		return nil, err
	}
	s.items = items
	s.at = s.now()
	return copyServices(items), nil
}

// Invalidate 清空缓存，下一次 List 必定重新拉取。
func (s *ServiceCache) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
	s.at = time.Time{}
}

func copyServices(in []Service) []Service {
	out := make([]Service, len(in))
	copy(out, in)
	return out
}
