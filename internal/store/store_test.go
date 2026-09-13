package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("首次 Open: %v", err)
	}
	if _, err := first.UpdateSettings(context.Background(), func(s *Settings) {
		s.ReportTime = "07:30"
	}); err != nil {
		t.Fatalf("写入设置: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("二次 Open: %v", err)
	}
	defer func() { _ = second.Close() }()

	// schema 与设置行都必须幂等：已有数据不能被重置
	got, err := second.Settings(context.Background())
	if err != nil {
		t.Fatalf("二次读取设置: %v", err)
	}
	if got.ReportTime != "07:30" {
		t.Errorf("二次 Open 把设置重置成了 %q", got.ReportTime)
	}
}

func TestSettingsDefaultsAndUpdate(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	def, err := s.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if !def.ReportEnabled || !def.TrafficAlertEnabled || !def.ExpiryAlertEnabled {
		t.Errorf("默认开关应为全开: %+v", def)
	}
	if def.ReportTime != "09:00" || def.ExpiryDays != 3 {
		t.Errorf("默认值与 schema 不一致: %+v", def)
	}
	if len(def.TrafficThresholds) != 2 || def.TrafficThresholds[0] != 80 || def.TrafficThresholds[1] != 100 {
		t.Errorf("默认阈值 = %v", def.TrafficThresholds)
	}

	updated, err := s.UpdateSettings(ctx, func(st *Settings) {
		st.ReportEnabled = false
		st.ReportTime = "06:15"
		st.TrafficThresholds = []int{90, 70, 90}
		st.ExpiryDays = 7
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if updated.ReportEnabled {
		t.Error("report_enabled 未写入")
	}
	if updated.ReportTime != "06:15" || updated.ExpiryDays != 7 {
		t.Errorf("更新后 = %+v", updated)
	}
	// 阈值应当被排序去重
	if len(updated.TrafficThresholds) != 2 || updated.TrafficThresholds[0] != 70 || updated.TrafficThresholds[1] != 90 {
		t.Errorf("阈值未规范化: %v", updated.TrafficThresholds)
	}

	// 重新打开确认落盘
	again, err := s.Settings(ctx)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if again.ReportTime != "06:15" || again.ReportEnabled {
		t.Errorf("重新读取 = %+v", again)
	}
}

func TestSettingsNormalizeRepairsDirtyValues(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*Settings)
		check  func(Settings) bool
	}{
		{"非法时间", func(st *Settings) { st.ReportTime = "25:99" },
			func(got Settings) bool { return got.ReportTime == defaultReportTime }},
		{"越界与空阈值", func(st *Settings) { st.TrafficThresholds = []int{-5, 200} },
			func(got Settings) bool { return len(got.TrafficThresholds) == 2 && got.TrafficThresholds[0] == 80 }},
		{"负的提前天数", func(st *Settings) { st.ExpiryDays = -1 },
			func(got Settings) bool { return got.ExpiryDays == 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.UpdateSettings(ctx, tc.mutate)
			if err != nil {
				t.Fatalf("UpdateSettings: %v", err)
			}
			if !tc.check(got) {
				t.Errorf("规范化结果不符预期: %+v", got)
			}
		})
	}
}

func TestChatsLifecycleAndGreeting(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if allowed, err := s.IsAllowed(ctx, 42); err != nil || allowed {
		t.Fatalf("空表时 IsAllowed = %v, %v", allowed, err)
	}
	if need, err := s.NeedGreeting(ctx, 42); err != nil || need {
		t.Fatalf("不在白名单内不应需要招呼: %v, %v", need, err)
	}

	if err := s.AllowChat(ctx, 42); err != nil {
		t.Fatalf("AllowChat: %v", err)
	}
	if allowed, _ := s.IsAllowed(ctx, 42); !allowed {
		t.Fatal("加入后 IsAllowed 应为 true")
	}
	if need, _ := s.NeedGreeting(ctx, 42); !need {
		t.Fatal("新加入的 chat 需要招呼")
	}

	if err := s.MarkGreeted(ctx, 42); err != nil {
		t.Fatalf("MarkGreeted: %v", err)
	}
	if need, _ := s.NeedGreeting(ctx, 42); need {
		t.Fatal("招呼后不应再需要招呼")
	}

	// 重复加入不能重置招呼状态，否则每次重启都会重新欢迎
	if err := s.AllowChat(ctx, 42); err != nil {
		t.Fatalf("重复 AllowChat: %v", err)
	}
	if need, _ := s.NeedGreeting(ctx, 42); need {
		t.Fatal("重复加入重置了招呼状态")
	}

	if ids, _ := s.Chats(ctx); len(ids) != 1 || ids[0] != 42 {
		t.Errorf("Chats = %v", ids)
	}

	if err := s.DenyChat(ctx, 42); err != nil {
		t.Fatalf("DenyChat: %v", err)
	}
	if allowed, _ := s.IsAllowed(ctx, 42); allowed {
		t.Fatal("移除后 IsAllowed 应为 false")
	}
}

func TestSeedOwnerIfEmpty(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if err := s.SeedOwnerIfEmpty(ctx, 7); err != nil {
		t.Fatalf("SeedOwnerIfEmpty: %v", err)
	}
	if allowed, _ := s.IsAllowed(ctx, 7); !allowed {
		t.Fatal("空白名单时应播种 owner")
	}

	// 白名单非空时不再播种，否则用户主动 /deny owner 会被重启加回来
	if err := s.SeedOwnerIfEmpty(ctx, 99); err != nil {
		t.Fatalf("SeedOwnerIfEmpty: %v", err)
	}
	if allowed, _ := s.IsAllowed(ctx, 99); allowed {
		t.Fatal("白名单非空时不应再播种")
	}

	// owner_chat_id 为 0 是允许的（安装时留空），不能报错也不能写入 0
	if err := s.SeedOwnerIfEmpty(ctx, 0); err != nil {
		t.Fatalf("owner 为 0 时不应报错: %v", err)
	}
	if allowed, _ := s.IsAllowed(ctx, 0); allowed {
		t.Fatal("不应写入 chat_id 0")
	}

	// 白名单被清空后，重启应能重新兜底，避免把自己锁在外面
	if err := s.DenyChat(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedOwnerIfEmpty(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := s.IsAllowed(ctx, 7); !allowed {
		t.Fatal("白名单清空后应重新播种 owner")
	}
}

func TestSaveAndQueryDailyUsage(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	buckets := []DailyUsage{
		{Day: "2026-09-10", InBytes: 100, OutBytes: 200, FetchedAt: now},
		{Day: "2026-09-11", InBytes: 300, OutBytes: 400, FetchedAt: now},
	}
	if err := s.SaveDailyUsage(ctx, 1, buckets); err != nil {
		t.Fatalf("SaveDailyUsage: %v", err)
	}

	// 覆盖写：BreaCloud 的日桶会滚动修正，后到的值才是权威值
	if err := s.SaveDailyUsage(ctx, 1, []DailyUsage{
		{Day: "2026-09-11", InBytes: 3000, OutBytes: 4000, FetchedAt: now.Add(time.Minute)},
	}); err != nil {
		t.Fatalf("覆盖写入: %v", err)
	}

	got, err := s.DailyUsage(ctx, 1, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatalf("DailyUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应返回 2 条，实际 %d", len(got))
	}
	// 按日期倒序
	if got[0].Day != "2026-09-11" || got[0].InBytes != 3000 || got[0].OutBytes != 4000 {
		t.Errorf("覆盖写入未生效或顺序错误: %+v", got[0])
	}
	if got[1].Day != "2026-09-10" {
		t.Errorf("顺序错误: %+v", got)
	}

	// 区间过滤
	narrow, err := s.DailyUsage(ctx, 1, "2026-09-11", "2026-09-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(narrow) != 1 || narrow[0].Day != "2026-09-11" {
		t.Errorf("区间过滤失败: %+v", narrow)
	}

	// 服务之间互不干扰
	if err := s.SaveDailyUsage(ctx, 2, []DailyUsage{{Day: "2026-09-10", InBytes: 7, FetchedAt: now}}); err != nil {
		t.Fatal(err)
	}
	other, _ := s.DailyUsage(ctx, 2, "2026-09-01", "2026-09-30")
	if len(other) != 1 || other[0].InBytes != 7 {
		t.Errorf("服务 2 的数据被污染: %+v", other)
	}

	// 空切片是空操作
	if err := s.SaveDailyUsage(ctx, 1, nil); err != nil {
		t.Fatalf("空切片应被接受: %v", err)
	}
	// 缺日期的记录被跳过，而不是写出一行 day=''
	if err := s.SaveDailyUsage(ctx, 1, []DailyUsage{{InBytes: 1}}); err != nil {
		t.Fatalf("缺日期应被跳过: %v", err)
	}
	if all, _ := s.DailyUsage(ctx, 1, "", "9999-12-31"); len(all) != 2 {
		t.Errorf("跳过逻辑失效: %+v", all)
	}
}

func TestAlertStateArmsAndDisarms(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if at, err := s.AlertState(ctx, 1, 80); err != nil || at != nil {
		t.Fatalf("初始应为已武装: %v, %v", at, err)
	}

	now := time.Now().Truncate(time.Second)
	if err := s.SetAlerted(ctx, 1, 80, &now); err != nil {
		t.Fatalf("SetAlerted: %v", err)
	}
	at, err := s.AlertState(ctx, 1, 80)
	if err != nil {
		t.Fatalf("AlertState: %v", err)
	}
	if at == nil || !at.Equal(now) {
		t.Fatalf("写入后 AlertState = %v", at)
	}

	// 阈值之间相互独立
	if other, _ := s.AlertState(ctx, 1, 100); other != nil {
		t.Fatal("阈值之间不应互相影响")
	}
	// 服务之间相互独立
	if other, _ := s.AlertState(ctx, 2, 80); other != nil {
		t.Fatal("服务之间不应互相影响")
	}

	if err := s.SetAlerted(ctx, 1, 80, nil); err != nil {
		t.Fatalf("重新武装: %v", err)
	}
	if at, _ := s.AlertState(ctx, 1, 80); at != nil {
		t.Fatal("重新武装后应为 nil")
	}
}

func TestJobRuns(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if ran, err := s.JobRan(ctx, "2026-09-13", "daily"); err != nil || ran {
		t.Fatalf("初始应为未运行: %v, %v", ran, err)
	}
	if err := s.MarkJobRan(ctx, "2026-09-13", "daily"); err != nil {
		t.Fatalf("MarkJobRan: %v", err)
	}
	if ran, _ := s.JobRan(ctx, "2026-09-13", "daily"); !ran {
		t.Fatal("写入后应为已运行")
	}
	// 不同日期、不同任务互不影响
	if ran, _ := s.JobRan(ctx, "2026-09-14", "daily"); ran {
		t.Error("日期之间不应互相影响")
	}
	if ran, _ := s.JobRan(ctx, "2026-09-13", "expiry"); ran {
		t.Error("任务之间不应互相影响")
	}
	// 重复写入是空操作
	if err := s.MarkJobRan(ctx, "2026-09-13", "daily"); err != nil {
		t.Fatalf("重复 MarkJobRan 应无错: %v", err)
	}
}

func TestPrune(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	now := time.Now()

	if err := s.SaveDailyUsage(ctx, 1, []DailyUsage{
		{Day: "2025-01-01", InBytes: 1, FetchedAt: now},
		{Day: "2026-09-11", InBytes: 2, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneDailyUsage(ctx, "2026-01-01"); err != nil {
		t.Fatalf("PruneDailyUsage: %v", err)
	}
	left, _ := s.DailyUsage(ctx, 1, "", "9999-12-31")
	if len(left) != 1 || left[0].Day != "2026-09-11" {
		t.Errorf("清理结果 = %+v", left)
	}

	if err := s.MarkJobRan(ctx, "2025-01-01", "daily"); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneJobRuns(ctx, "2026-01-01"); err != nil {
		t.Fatalf("PruneJobRuns: %v", err)
	}
	if ran, _ := s.JobRan(ctx, "2025-01-01", "daily"); ran {
		t.Error("任务标记未被清理")
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("空路径应报错")
	}
}
