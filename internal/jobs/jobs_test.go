package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/md"
	"github.com/lay-g/breacloud-tg-bot/internal/notify"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// fakeBot 是 BreaCloud API 的替身：只需覆盖任务用到的那几个接口。
type fakeBot struct {
	mu       sync.Mutex
	services []map[string]any
	// daily 按服务 id 给出「日期 -> 字节数」
	daily map[int64]map[string]int64
	// traffic 按服务 id 给出周期用量
	traffic map[int64]map[string]any
	// historyFail 让指定服务的日用量请求返回 500，用于测试失败统计
	historyFail map[int64]bool

	historyRequests int32
	trafficRequests int32
}

func (f *fakeBot) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		writeData := func(v any) {
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("序列化: %v", err)
			}
			_, _ = fmt.Fprintf(w, `{"code":"OK","message":"ok","data":%s}`, raw)
		}
		path := r.URL.Path
		switch {
		case path == "/services":
			if r.URL.Query().Get("page") != "1" {
				writeData(map[string]any{"services": []any{}})
				return
			}
			writeData(map[string]any{"services": f.services})

		case strings.HasSuffix(path, "/traffic-history"):
			atomic.AddInt32(&f.historyRequests, 1)
			id := pathID(t, path)
			if f.historyFail[id] {
				// 用 4xx 而不是 5xx：5xx 会触发客户端的退避重试，让测试白等几秒，
				// 而重试逻辑本身已经在 breacloud 包里覆盖过了。
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"ERR_INVALID_REQUEST","message":"boom","data":null}`))
				return
			}
			f.mu.Lock()
			days := f.daily[id]
			f.mu.Unlock()
			var list []map[string]any
			for day, total := range days {
				list = append(list, map[string]any{"bucket": day, "in_bytes": total / 2, "out_bytes": total - total/2})
			}
			sortDailyDesc(list)
			writeData(map[string]any{"range": "week", "daily": list})

		case strings.HasSuffix(path, "/traffic"):
			atomic.AddInt32(&f.trafficRequests, 1)
			id := pathID(t, path)
			f.mu.Lock()
			payload, ok := f.traffic[id]
			f.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"ERR_NOT_FOUND","message":"not found","data":null}`))
				return
			}
			writeData(map[string]any{"traffic": payload})

		case strings.HasSuffix(path, "/tasks"):
			writeData(map[string]any{"tasks": []any{}})

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"ERR_NOT_FOUND","message":"not found","data":null}`))
		}
	}
}

func pathID(t *testing.T, path string) int64 {
	t.Helper()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		t.Fatalf("无法从 %q 解析服务 id", path)
	}
	var id int64
	if _, err := fmt.Sscanf(parts[1], "%d", &id); err != nil {
		t.Fatalf("解析服务 id 失败: %v", err)
	}
	return id
}

func sortDailyDesc(list []map[string]any) {
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j]["bucket"].(string) > list[i]["bucket"].(string) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
}

// collectNotifier 记录发送出去的消息。
type collectNotifier struct {
	mu       sync.Mutex
	messages []string
	chats    []int64
	failWith error
}

func (c *collectNotifier) Send(_ context.Context, chatID int64, text string) error {
	if c.failWith != nil {
		return c.failWith
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chats = append(c.chats, chatID)
	c.messages = append(c.messages, text)
	return nil
}

func (c *collectNotifier) Broadcast(ctx context.Context, text string) error {
	return c.Send(ctx, 100, text)
}

func (c *collectNotifier) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		return ""
	}
	return c.messages[len(c.messages)-1]
}

func (c *collectNotifier) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.messages)
}

// newFixture 组装一套任务依赖。
func newFixture(t *testing.T, fake *fakeBot, now time.Time) (Deps, *collectNotifier) {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	client := breacloud.New(config.BreaCloudConfig{
		BaseURL: server.URL, APIToken: "bll_test", Concurrency: 4, Timeout: 5 * time.Second,
	})
	recorder := &collectNotifier{}
	deps := Deps{
		Store:  st,
		API:    client,
		Cache:  breacloud.NewServiceCache(client, time.Minute),
		Notify: recorder,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return now },
	}
	return deps, recorder
}

func service(id int64, name string, extra map[string]any) map[string]any {
	out := map[string]any{
		"id": id, "domain_name": name, "status": "active", "region_name": "Los Angeles",
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestBuildDailyReport(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{
			service(1, "vps-a", nil),
			service(2, "vps-b", nil),
			service(3, "vps-c", nil),
		},
		daily: map[int64]map[string]int64{
			1: {"2026-09-12": 10 << 30, "2026-09-11": 5 << 30},
			2: {"2026-09-12": 30 << 30, "2026-09-11": 25 << 30},
			3: {"2026-09-12": 20 << 30, "2026-09-11": 20 << 30},
		},
	}
	deps, _ := newFixture(t, fake, now)

	text, err := BuildDailyReport(context.Background(), deps)
	if err != nil {
		t.Fatalf("BuildDailyReport: %v", err)
	}
	plain := md.Unescape(text)
	for _, want := range []string{
		"2026-09-12", // 目标是 UTC+8 的昨天，不是今天
		"60.00 GB",   // 10+30+20
		"1. vps-b",   // 按用量降序
		"10.00 GB",   // 第一名
		"vps-c（Los Angeles）",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("报告缺少 %q:\n%s", want, text)
		}
	}
	// 前一日 5+25+20 = 50 GB，环比 +20%
	if !strings.Contains(plain, "+20.0%") {
		t.Errorf("环比计算异常:\n%s", text)
	}
	// 结构与转义：总量标签是粗体；日期放进等宽实体（code 内部只需转义反引号与反斜杠）
	if !strings.Contains(text, "*全网总量：*") || !strings.Contains(text, "`2026-09-12`") {
		t.Errorf("MarkdownV2 结构异常:\n%s", text)
	}
}

func TestSweepWritesDailyUsageCache(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		daily:    map[int64]map[string]int64{1: {"2026-09-12": 8 << 30, "2026-09-11": 4 << 30}},
	}
	deps, _ := newFixture(t, fake, now)

	if _, err := BuildDailyReport(context.Background(), deps); err != nil {
		t.Fatalf("BuildDailyReport: %v", err)
	}
	// 一次 range=week 请求应当把 8 天都写进缓存，而不是只写目标日
	rows, err := deps.Store.DailyUsage(context.Background(), 1, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatalf("DailyUsage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("缓存行数 = %d: %+v", len(rows), rows)
	}
	// 按日期倒序
	if rows[0].Day != "2026-09-12" || rows[0].InBytes+rows[0].OutBytes != 8<<30 {
		t.Errorf("缓存内容异常: %+v", rows[0])
	}
}

func TestReportCountsFailedServices(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{
			service(1, "ok", nil),
			service(2, "broken", nil), // 请求失败
			service(3, "no-traffic", nil),
		},
		daily:       map[int64]map[string]int64{1: {"2026-09-12": 5 << 30}},
		historyFail: map[int64]bool{2: true},
	}
	deps, _ := newFixture(t, fake, now)

	text, err := BuildDailyReport(context.Background(), deps)
	if err != nil {
		t.Fatalf("BuildDailyReport: %v", err)
	}
	if !strings.Contains(md.Unescape(text), "1 台机器") {
		t.Errorf("应标注失败台数:\n%s", text)
	}
	// 当天没有日桶的机器是正常情况，不该被算成失败
	if strings.Contains(md.Unescape(text), "2 台机器") {
		t.Errorf("无日桶的机器被误判为失败:\n%s", text)
	}
}

func TestTrafficAlertFiresOnceAndRearms(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		traffic: map[int64]map[string]any{
			1: {"quota_gb": 100, "in_bytes": 90 << 30, "out_bytes": 0, "period_end": "2026-10-01T00:00:00Z"},
		},
	}
	deps, recorder := newFixture(t, fake, now)
	// 只开一个阈值，方便断言
	if _, err := deps.Store.UpdateSettings(context.Background(), func(s *store.Settings) {
		s.TrafficThresholds = []int{80}
	}); err != nil {
		t.Fatal(err)
	}

	// 第一次：越过 80% 应推送
	alerts, err := RunTrafficCheck(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunTrafficCheck: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Percent != 90 {
		t.Fatalf("预警 = %+v", alerts)
	}
	if err := BroadcastTrafficAlerts(context.Background(), deps, alerts); err != nil {
		t.Fatal(err)
	}
	if recorder.count() != 1 || !strings.Contains(md.Unescape(recorder.last()), "vps-a") {
		t.Fatalf("消息 = %q", recorder.last())
	}

	// 第二次：仍在阈值上，不应重复推送
	if alerts, err := RunTrafficCheck(context.Background(), deps); err != nil {
		t.Fatal(err)
	} else if len(alerts) != 0 {
		t.Fatalf("同一阈值不应重复推送: %+v", alerts)
	}
	if recorder.count() != 1 {
		t.Errorf("消息数 = %d, want 1", recorder.count())
	}

	// 用量回落到阈值以下：重新武装
	fake.mu.Lock()
	fake.traffic[1] = map[string]any{"quota_gb": 100, "in_bytes": 10 << 30, "out_bytes": 0}
	fake.mu.Unlock()
	if alerts, err := RunTrafficCheck(context.Background(), deps); err != nil {
		t.Fatal(err)
	} else if len(alerts) != 0 {
		t.Fatalf("回落后不应推送: %+v", alerts)
	}

	// 再次越线：允许重新推送
	fake.mu.Lock()
	fake.traffic[1] = map[string]any{"quota_gb": 100, "in_bytes": 85 << 30, "out_bytes": 0}
	fake.mu.Unlock()
	alerts, err = RunTrafficCheck(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("回落再越线应重新推送: %+v", alerts)
	}
}

func TestTrafficAlertSkipsUnlimitedAndZeroQuota(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "unlimited", nil), service(2, "zero", nil)},
		traffic: map[int64]map[string]any{
			1: {"quota_gb": 1000, "in_bytes": 999 << 30, "unlimited": true},
			2: {"quota_gb": 0, "in_bytes": 10 << 30},
		},
	}
	deps, _ := newFixture(t, fake, now)

	alerts, err := RunTrafficCheck(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunTrafficCheck: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("不限量与零配额都应跳过: %+v", alerts)
	}
}

func TestTrafficCheckHonoursSwitch(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		traffic:  map[int64]map[string]any{1: {"quota_gb": 100, "in_bytes": 99 << 30}},
	}
	deps, _ := newFixture(t, fake, now)
	if _, err := deps.Store.UpdateSettings(context.Background(), func(s *store.Settings) {
		s.TrafficAlertEnabled = false
	}); err != nil {
		t.Fatal(err)
	}

	alerts, err := RunTrafficCheck(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("开关关闭时不应推送: %+v", alerts)
	}
	// 关闭时连请求都不该发
	if n := atomic.LoadInt32(&fake.trafficRequests); n != 0 {
		t.Errorf("开关关闭时仍发出了 %d 个请求", n)
	}
}

func TestExpiryCheckWindowsAndSorting(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{services: []map[string]any{
		service(1, "due-in-2", map[string]any{
			"billing_mode": "periodic", "next_due_date": "2026-09-15T00:00:00Z", "auto_renew_enabled": false}),
		service(2, "due-in-1", map[string]any{
			"billing_mode": "periodic", "next_due_date": "2026-09-14T00:00:00Z", "auto_renew_enabled": true}),
		service(3, "too-far", map[string]any{
			"billing_mode": "periodic", "next_due_date": "2026-10-30T00:00:00Z"}),
		service(4, "hourly", map[string]any{
			"billing_mode": "usage", "expire_at": "2026-09-14T00:00:00Z"}),
		service(5, "canceled", map[string]any{
			"billing_mode": "periodic", "next_due_date": "2026-09-15T00:00:00Z",
			"renew_canceled_at": "2026-09-01T00:00:00Z"}),
	}}
	deps, recorder := newFixture(t, fake, now)
	if _, err := deps.Store.UpdateSettings(context.Background(), func(s *store.Settings) {
		s.ExpiryDays = 3
	}); err != nil {
		t.Fatal(err)
	}

	items, err := RunExpiryCheck(context.Background(), deps)
	if err != nil {
		t.Fatalf("RunExpiryCheck: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("应命中 3 台（含已关闭续费的），实际 %d: %+v", len(items), items)
	}
	if items[0].Name != "due-in-1" || items[0].DaysLeft != 1 {
		t.Errorf("应按剩余天数升序: %+v", items)
	}
	var canceled *ExpiryItem
	for i := range items {
		if items[i].Name == "canceled" {
			canceled = &items[i]
		}
	}
	if canceled == nil || !canceled.Canceled {
		t.Errorf("已关闭续费的服务应被标记: %+v", items)
	}

	if err := BroadcastExpiryAlerts(context.Background(), deps, items, 3); err != nil {
		t.Fatal(err)
	}
	plain := md.Unescape(recorder.last())
	if !strings.Contains(plain, "将自动续费") || !strings.Contains(plain, "到期释放且不再续费") {
		t.Errorf("文案未区分续费状态:\n%s", recorder.last())
	}
}

func TestExpiryCheckSilentWhenEmpty(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{services: []map[string]any{
		service(1, "far", map[string]any{"billing_mode": "periodic", "next_due_date": "2027-01-01T00:00:00Z"}),
	}}
	deps, recorder := newFixture(t, fake, now)

	items, err := RunExpiryCheck(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := BroadcastExpiryAlerts(context.Background(), deps, items, 3); err != nil {
		t.Fatal(err)
	}
	if recorder.count() != 0 {
		t.Errorf("没有临期服务时不该发消息，实际发了 %d 条", recorder.count())
	}
}

func TestDueForDaily(t *testing.T) {
	at := func(hhmm string) time.Time {
		parsed, err := time.Parse("2006-01-02 15:04", "2026-09-13 "+hhmm)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	cases := []struct {
		now, report string
		want        bool
	}{
		{"08:59", "09:00", false},
		{"09:00", "09:00", true},
		{"23:00", "09:00", true},
		{"00:30", "23:30", false}, // 跨零点时当日尚未到点
		{"10:00", "", true},       // 空值回退默认 09:00
	}
	for _, tc := range cases {
		if got := dueForDaily(at(tc.now), tc.report); got != tc.want {
			t.Errorf("dueForDaily(%s, %q) = %v, want %v", tc.now, tc.report, got, tc.want)
		}
	}
}

func TestSchedulerDailyJobIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		daily:    map[int64]map[string]int64{1: {"2026-09-12": 5 << 30}},
	}
	deps, recorder := newFixture(t, fake, now)

	scheduler := NewScheduler(deps, time.Hour)
	ctx := context.Background()

	scheduler.maybeRunDaily(ctx)
	if recorder.count() != 1 {
		t.Fatalf("首次执行应发一条日报，实际 %d", recorder.count())
	}
	before := atomic.LoadInt32(&fake.historyRequests)

	// 同一天再跑：不应重复发送，也不应重新取数
	scheduler.maybeRunDaily(ctx)
	if recorder.count() != 1 {
		t.Errorf("重复执行发送了消息，共 %d 条", recorder.count())
	}
	if after := atomic.LoadInt32(&fake.historyRequests); after != before {
		t.Errorf("重复执行重新取数了：%d -> %d", before, after)
	}
}

func TestSchedulerSkipsWhenBothSwitchesOff(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, bcZone)
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		daily:    map[int64]map[string]int64{1: {"2026-09-12": 5 << 30}},
	}
	deps, recorder := newFixture(t, fake, now)
	if _, err := deps.Store.UpdateSettings(context.Background(), func(s *store.Settings) {
		s.ReportEnabled = false
		s.ExpiryAlertEnabled = false
	}); err != nil {
		t.Fatal(err)
	}

	NewScheduler(deps, time.Hour).maybeRunDaily(context.Background())
	if recorder.count() != 0 {
		t.Errorf("两个开关都关掉时不该发消息，实际 %d 条", recorder.count())
	}
	if n := atomic.LoadInt32(&fake.historyRequests); n != 0 {
		t.Errorf("两个开关都关掉时不该取数，实际 %d 次请求", n)
	}
}

func TestSchedulerWaitsForReportTime(t *testing.T) {
	now := time.Date(2026, 9, 13, 8, 0, 0, 0, bcZone) // 比 09:00 早
	fake := &fakeBot{
		services: []map[string]any{service(1, "vps-a", nil)},
		daily:    map[int64]map[string]int64{1: {"2026-09-12": 5 << 30}},
	}
	deps, recorder := newFixture(t, fake, now)

	NewScheduler(deps, time.Hour).maybeRunDaily(context.Background())
	if recorder.count() != 0 {
		t.Errorf("未到通知时间不应发送，实际 %d 条", recorder.count())
	}
}

func TestNotifierMatchesDryRunContract(t *testing.T) {
	// DryRun 必须满足同一个接口，否则 --dry-run 无法替代真实发送
	var _ notify.Notifier = (*notify.DryRun)(nil)
}
