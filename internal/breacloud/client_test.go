package breacloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
)

func newTestClient(t *testing.T, baseURL string, concurrency int) *Client {
	t.Helper()
	c := New(config.BreaCloudConfig{
		BaseURL:     baseURL,
		APIToken:    "bll_test",
		Concurrency: concurrency,
		Timeout:     5 * time.Second,
	})
	c.backoffBase = time.Millisecond
	return c
}

// envelope 按统一信封写一个成功响应。
func envelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("序列化测试数据: %v", err)
	}
	_, _ = fmt.Fprintf(w, `{"code":"OK","message":"ok","data":%s}`, raw)
}

// pagedHandler 只对 page=1 返回给定服务，其余页返回空，模拟正常的分页行为。
// 响应没有 total 字段，客户端只能翻到空页为止，因此每个用例都必须给出空页。
func pagedHandler(rawJSON string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte(`{"code":"OK","message":"ok","data":{"services":[]}}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":"OK","message":"ok","data":{"services":%s}}`, rawJSON)
	}
}

func servicesJSON(ids ...int64) map[string]any {
	list := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		list = append(list, map[string]any{"id": id, "domain_name": fmt.Sprintf("vps-%d", id)})
	}
	return map[string]any{"services": list}
}

func TestListServicesPaginatesUntilEmptyPage(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			envelope(t, w, servicesJSON(1, 2))
		case "2":
			envelope(t, w, servicesJSON(3))
		default:
			envelope(t, w, servicesJSON())
		}
	}))
	defer server.Close()

	got, err := newTestClient(t, server.URL, 5).ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应拿到 3 台，实际 %d", len(got))
	}
	// 最后一页必然多一次请求：响应没有 total，只能靠空页判断结束
	if n := atomic.LoadInt32(&requests); n != 3 {
		t.Errorf("请求次数 = %d, want 3", n)
	}
}

func TestListServicesDedupesWhenServerIgnoresPage(t *testing.T) {
	// 服务端忽略了 page 参数，永远返回第一页；不去重就会把同一页累加 50 次。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope(t, w, servicesJSON(1, 2))
	}))
	defer server.Close()

	c := newTestClient(t, server.URL, 5)
	_, err := c.ListServices(context.Background())
	if err == nil {
		t.Fatal("翻页始终非空时应报错中止，而不是静默返回")
	}
}

func TestEnvelopeErrorBecomesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"ERR_FORBIDDEN","message":"API Token 缺少权限: 需要 client.svc.read.traffic","data":null}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL, 5).GetTraffic(context.Background(), 1)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T, want *APIError", err)
	}
	if apiErr.Status != 403 || apiErr.Code != "ERR_FORBIDDEN" {
		t.Errorf("APIError = %+v", apiErr)
	}
	if !apiErr.Forbidden() {
		t.Error("Forbidden() 应为 true")
	}
	// 原始消息要能直接展示给用户，包含缺失的 scope
	if !strings.Contains(apiErr.Message, "client.svc.read.traffic") {
		t.Errorf("消息丢失了 scope 信息: %q", apiErr.Message)
	}
}

func TestNonJSONResponseKeepsSnippet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer server.Close()

	c := newTestClient(t, server.URL, 5)
	c.backoffBase = time.Millisecond
	_, err := c.ListServices(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型 = %T", err)
	}
	if !strings.Contains(apiErr.Body, "502 Bad Gateway") {
		t.Errorf("原始响应片段丢失: %+v", apiErr)
	}
}

func TestRetriesOnServerError(t *testing.T) {
	var failures int32
	page := pagedHandler(`[{"id":1,"domain_name":"vps-1"}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&failures, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"ERR_INTERNAL","message":"boom","data":null}`))
			return
		}
		page(w, r)
	}))
	defer server.Close()

	got, err := newTestClient(t, server.URL, 5).ListServices(context.Background())
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("结果 = %+v", got)
	}
	// 前两次 500 被重试掉了（maxRetries=3），之后的两条请求是正常的分页行为
	if n := atomic.LoadInt32(&failures); n != 4 {
		t.Errorf("总请求数 = %d, want 4（2 次失败 + 2 次分页）", n)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"ERR_INVALID_REQUEST","message":"Invalid request","data":null}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL, 5).ListServices(context.Background())
	if err == nil {
		t.Fatal("400 应返回错误")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("4xx 不应重试，尝试次数 = %d", n)
	}
}

func TestPowerActionIsNotRetried(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"ERR_INTERNAL","message":"boom","data":null}`))
	}))
	defer server.Close()

	// 电源操作非幂等：即使 5xx 也只能尝试一次
	_ = newTestClient(t, server.URL, 5).DoAction(context.Background(), 1, "cold_reboot")
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("电源操作被重试了 %d 次", n)
	}
}

func TestPowerActionSendsBody(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		envelope(t, w, map[string]any{"queued": true, "action": gotBody["action"]})
	}))
	defer server.Close()

	if err := newTestClient(t, server.URL, 5).DoAction(context.Background(), 42, "shutdown"); err != nil {
		t.Fatalf("DoAction: %v", err)
	}
	if gotPath != "/services/42/actions" {
		t.Errorf("路径 = %q", gotPath)
	}
	if gotBody["action"] != "shutdown" {
		t.Errorf("请求体 = %+v", gotBody)
	}
}

func TestConcurrencyLimitIsRespected(t *testing.T) {
	var inFlight, maxInFlight int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if cur <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, cur) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		envelope(t, w, map[string]any{"service": map[string]any{"id": 1}, "resource": map[string]any{}})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 2)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.GetService(context.Background(), 1)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > 2 {
		t.Errorf("并发峰值 = %d, 超过配置的 2", got)
	}
}

func TestTrafficUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		envelope(t, w, map[string]any{"traffic": map[string]any{
			"period_start": "2026-09-01T00:00:00Z",
			"period_end":   "2026-10-01T00:00:00Z",
			"quota_gb":     2000,
			"used_gb":      429,
			"in_bytes":     232330626871,
			"out_bytes":    227569964859,
			"unlimited":    false,
		}})
	}))
	defer server.Close()

	tr, err := newTestClient(t, server.URL, 5).GetTraffic(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	if tr.Total() != 459900591730 {
		t.Errorf("Total = %d", tr.Total())
	}
	// quota_gb 是 GiB：2000 << 30
	if tr.QuotaBytes() != 2000<<30 {
		t.Errorf("QuotaBytes = %d", tr.QuotaBytes())
	}
	// 用字节反推的 GiB 应与接口给的 used_gb 吻合：接口对 428.3 给出了 429，说明它做了取整，
	// 因此允许 1 GiB 的偏差，但量级必须一致（用 10^9 换算会得到 459，明显不符）。
	if got := tr.Total() >> 30; got < tr.UsedGB-1 || got > tr.UsedGB {
		t.Errorf("字节换算 GiB = %d, 接口 used_gb = %d", got, tr.UsedGB)
	}
}

func TestGetTrafficWeekFindsDay(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		envelope(t, w, map[string]any{
			"range": "week",
			"daily": []map[string]any{
				{"bucket": "2026-09-13", "in_bytes": 100, "out_bytes": 200},
				{"bucket": "2026-09-12", "in_bytes": 300, "out_bytes": 400},
			},
		})
	}))
	defer server.Close()

	h, err := newTestClient(t, server.URL, 5).GetTrafficWeek(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetTrafficWeek: %v", err)
	}
	// 必须用 range=week：range=day 的昨天桶会被 24 小时窗口截断
	if !strings.Contains(gotQuery, "range=week") {
		t.Errorf("查询参数 = %q", gotQuery)
	}
	b, ok := h.FindDay("2026-09-12")
	if !ok {
		t.Fatal("未找到 2026-09-12")
	}
	if b.Total() != 700 {
		t.Errorf("日桶总量 = %d", b.Total())
	}
	if _, ok := h.FindDay("2026-09-01"); ok {
		t.Error("不应找到不存在的日期")
	}
}

func TestNullTimeFieldsAreTolerated(t *testing.T) {
	server := httptest.NewServer(pagedHandler(
		`[{"id":1,"expire_at":null,"renew_canceled_at":null,"next_due_date":"2027-08-23T00:00:00Z"}]`))
	defer server.Close()

	got, err := newTestClient(t, server.URL, 5).ListServices(context.Background())
	if err != nil {
		t.Fatalf("null 时间字段应被容忍: %v", err)
	}
	if len(got) != 1 || got[0].ExpireAt != "" || got[0].NextDueDate == "" {
		t.Errorf("解析结果 = %+v", got)
	}
	if !got[0].IsPeriodic() {
		t.Error("有 next_due_date 且 expire_at 为空时应判为周期机")
	}
}

func TestCacheReturnsCopyAndHonoursTTL(t *testing.T) {
	page := pagedHandler(`[{"id":1,"domain_name":"vps-1"}]`)
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		page(w, r)
	}))
	defer server.Close()

	now := time.Now()
	cache := NewServiceCache(newTestClient(t, server.URL, 5), time.Minute)
	cache.now = func() time.Time { return now }

	first, err := cache.List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 一次 List 会打两条请求（第一页 + 判空页），以它为基准计数
	base := atomic.LoadInt32(&requests)

	// 调用方修改返回值不应污染缓存
	first[0].DomainName = "被改了"

	second, err := cache.List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if second[0].DomainName == "被改了" {
		t.Error("缓存返回的是内部切片，调用方修改污染了缓存")
	}
	if n := atomic.LoadInt32(&requests); n != base {
		t.Errorf("TTL 内不应重新请求，请求数 = %d, want %d", n, base)
	}

	// 过期后重新拉取
	now = now.Add(2 * time.Minute)
	if _, err := cache.List(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&requests); n != base*2 {
		t.Errorf("TTL 过期后应重新请求，请求数 = %d, want %d", n, base*2)
	}

	// 显式刷新
	if _, err := cache.List(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&requests); n != base*3 {
		t.Errorf("force 应忽略 TTL，请求数 = %d, want %d", n, base*3)
	}
}

func TestCacheFallsBackToStaleDataOnError(t *testing.T) {
	var fail atomic.Bool
	page := pagedHandler(`[{"id":1,"domain_name":"vps-1"}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"ERR_INTERNAL","message":"boom","data":null}`))
			return
		}
		page(w, r)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 5)
	cache := NewServiceCache(client, time.Millisecond)
	if _, err := cache.List(context.Background(), false); err != nil {
		t.Fatalf("List: %v", err)
	}

	fail.Store(true)
	got, err := cache.List(context.Background(), true)
	if err == nil {
		t.Fatal("上游失败时应把错误返回给调用方")
	}
	// 过期列表比没有列表好：调用方拿到旧数据，同时能知道刷新失败了
	if len(got) != 1 {
		t.Errorf("应回退到旧数据，实际 %+v", got)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"ERR_INTERNAL","message":"boom","data":null}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 5)
	client.backoffBase = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := client.DoAction(ctx, 1, "reboot"); err == nil {
		t.Fatal("应返回错误")
	}
	if n := atomic.LoadInt32(&attempts); n > 1 {
		t.Errorf("ctx 结束时不应继续重试，尝试次数 = %d", n)
	}
}
