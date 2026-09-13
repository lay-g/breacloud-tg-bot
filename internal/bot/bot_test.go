package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/md"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

func TestParseCallbackRoundTrip(t *testing.T) {
	cases := []struct {
		kind string
		args []string
	}{
		{cbMain, []string{pageVPS}},
		{cbRegion, []string{"1", "3"}},
		{cbView, []string{"3600"}},
		{cbAction, []string{"3600", "cold_reboot"}},
		{cbSettings, []string{"threshold", "80"}},
	}
	for _, tc := range cases {
		data := EncodeCallback(tc.kind, tc.args...)
		got, ok := ParseCallback(data)
		if !ok {
			t.Fatalf("解析失败: %q", data)
		}
		if got.Kind != tc.kind || len(got.Args) != len(tc.args) {
			t.Errorf("%q -> %+v", data, got)
		}
		for i, want := range tc.args {
			if got.Arg(i) != want {
				t.Errorf("%q 第 %d 个参数 = %q, want %q", data, i, got.Arg(i), want)
			}
		}
		// 回调数据必须塞得进 Telegram 的 64 字节限制
		if len(data) > 64 {
			t.Errorf("回调数据过长(%d): %q", len(data), data)
		}
	}
}

func TestParseCallbackRejectsMalformed(t *testing.T) {
	for _, data := range []string{"", "m", "|x"} {
		if _, ok := ParseCallback(data); ok {
			t.Errorf("%q 不该被接受", data)
		}
	}
}

func TestCallbackArgInt(t *testing.T) {
	cb, _ := ParseCallback("v|3600")
	if id, ok := cb.ArgInt(0); !ok || id != 3600 {
		t.Errorf("ArgInt(0) = %d, %v", id, ok)
	}
	if _, ok := cb.ArgInt(1); ok {
		t.Error("越界参数应返回 false")
	}
	bad, _ := ParseCallback("v|abc")
	if _, ok := bad.ArgInt(0); ok {
		t.Error("非数字参数应返回 false")
	}
}

func TestCallbackDataIsShortEnoughForLongNames(t *testing.T) {
	// 区域名可能很长，回调里用下标而不是名字，就是为了避开 64 字节限制
	cb, _ := ParseCallback(cbRegionAt(12, 42))
	if len(EncodeCallback(cb.Kind, cb.Args...)) > 64 {
		t.Error("区域回调过长")
	}
}

func TestApplySettingToggles(t *testing.T) {
	base := store.Settings{
		ReportEnabled:       true,
		ReportTime:          "09:00",
		TrafficAlertEnabled: true,
		TrafficThresholds:   []int{80, 100},
		ExpiryAlertEnabled:  true,
		ExpiryDays:          3,
	}

	got := applySetting(base, "report", "toggle")
	if got.ReportEnabled {
		t.Error("报告开关未切换")
	}
	if applySetting(base, "traffic", "toggle").TrafficAlertEnabled {
		t.Error("流量预警开关未切换")
	}
	if applySetting(base, "expiry", "toggle").ExpiryAlertEnabled {
		t.Error("到期预警开关未切换")
	}

	if got := applySetting(base, "days", "5"); got.ExpiryDays != 5 {
		t.Errorf("提前天数 = %d", got.ExpiryDays)
	}
	// 非预设值不接受
	if got := applySetting(base, "days", "4"); got.ExpiryDays != 3 {
		t.Errorf("非预设天数被接受了: %d", got.ExpiryDays)
	}
	if got := applySetting(base, "days", "abc"); got.ExpiryDays != 3 {
		t.Errorf("非法天数被接受了: %d", got.ExpiryDays)
	}
}

func TestApplySettingThresholds(t *testing.T) {
	base := store.Settings{TrafficThresholds: []int{80, 100}}

	// 新增一个档位
	got := applySetting(base, "threshold", "90")
	if len(got.TrafficThresholds) != 3 {
		t.Fatalf("阈值 = %v", got.TrafficThresholds)
	}

	// 取消一个档位
	got = applySetting(base, "threshold", "80")
	if len(got.TrafficThresholds) != 1 || got.TrafficThresholds[0] != 100 {
		t.Fatalf("阈值 = %v", got.TrafficThresholds)
	}

	// 不能把最后一个档位也取消：那等于偷偷关掉了预警，而开关还显示开启
	got = applySetting(store.Settings{TrafficThresholds: []int{80}}, "threshold", "80")
	if len(got.TrafficThresholds) != 1 {
		t.Errorf("最后一个阈值被取消了: %v", got.TrafficThresholds)
	}

	// 非预设档位不接受
	got = applySetting(base, "threshold", "55")
	if len(got.TrafficThresholds) != 2 {
		t.Errorf("非预设阈值被接受了: %v", got.TrafficThresholds)
	}
}

func TestApplySettingReportTime(t *testing.T) {
	cases := []struct {
		from, action, want string
	}{
		{"09:00", "plus", "09:30"},
		{"09:00", "minus", "08:30"},
		{"23:30", "plus", "00:00"},
		{"00:00", "minus", "23:30"},
		{"", "plus", "09:30"},       // 脏数据退回默认值再平移
		{"09:00", "bogus", "09:00"}, // 未知动作不改变
	}
	for _, tc := range cases {
		got := applySetting(store.Settings{ReportTime: tc.from}, "report_time", tc.action)
		if got.ReportTime != tc.want {
			t.Errorf("%s %s -> %s, want %s", tc.from, tc.action, got.ReportTime, tc.want)
		}
	}
}

func TestGroupByRegion(t *testing.T) {
	services := []breacloud.Service{
		{ID: 3, DomainName: "c", RegionName: "Los Angeles"},
		{ID: 1, DomainName: "a", RegionName: "Frankfurt"},
		{ID: 2, DomainName: "b", RegionName: "Los Angeles"},
		{ID: 4, DomainName: "d"},
	}
	regions := GroupByRegion(services)
	if len(regions) != 3 {
		t.Fatalf("区域数 = %d: %+v", len(regions), regions)
	}
	// 按区域名排序，未分类排在中英文之间由字典序决定
	names := []string{regions[0].Name, regions[1].Name, regions[2].Name}
	want := []string{"Frankfurt", "Los Angeles", "未分类"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("区域顺序 = %v, want %v", names, want)
		}
	}
	// 同一区域内按名称稳定排序
	la := regions[1].Services
	if len(la) != 2 || la[0].DomainName != "b" || la[1].DomainName != "c" {
		t.Errorf("区域内排序 = %+v", la)
	}
}

func TestRenderRegionListPagination(t *testing.T) {
	var regions []Region
	for i := 0; i < regionPerPage+3; i++ {
		regions = append(regions, Region{Name: string(rune('A' + i)), Services: []breacloud.Service{{ID: int64(i)}}})
	}
	text, pages := RenderRegionList(regions, 0)
	if pages != 2 {
		t.Fatalf("页数 = %d", pages)
	}
	if !strings.Contains(text, "第 1 / 2 页") {
		t.Errorf("缺少分页提示:\n%s", text)
	}
	// 越界页码被钳到最后一页
	text, _ = RenderRegionList(regions, 99)
	if !strings.Contains(text, "第 2 / 2 页") {
		t.Errorf("越界页码未钳制:\n%s", text)
	}
}

func TestRenderRegionListEmpty(t *testing.T) {
	text, pages := RenderRegionList(nil, 0)
	if pages != 1 || !strings.Contains(text, "还没有") {
		t.Errorf("空列表渲染异常: %q, pages=%d", text, pages)
	}
}

func TestRenderServiceDetail(t *testing.T) {
	detail := breacloud.ServiceDetail{
		Service: breacloud.Service{
			ID: 3600, DomainName: "vps-gia-185197", Status: "active",
			RegionName: "Los Angeles", OSName: "Debian 12", BandwidthMbps: 600,
			NextDueDate: "2027-08-23T00:00:00Z", AutoRenew: false,
			BillingMode: "periodic",
		},
		Resource: breacloud.Resource{
			VMID: 11149, NodeName: "lax-gia-main-01", PrimaryIP: "38.105.28.165",
			CPUCores: 1, MemoryMB: 1024, DiskGB: 20,
		},
	}
	traffic := breacloud.Traffic{QuotaGB: 2000, InBytes: 232330626871, OutBytes: 227569964859}
	days := []store.DailyUsage{
		{Day: "2026-09-13", InBytes: 16158429527},
		{Day: "2026-09-12", InBytes: 30944777449},
	}

	text := RenderServiceDetail(detail, traffic, days)
	// 内容断言在「还原转义之后」的文本上做，避免测试被转义细节淹没
	plain := md.Unescape(text)
	for _, want := range []string{
		"vps-gia-185197", "38.105.28.165", "lax-gia-main-01", "Debian 12",
		"2000 GB", "2026-09-12", "2027-08-23", "需手动续费",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("详情缺少 %q:\n%s", want, text)
		}
	}
	// 百分比按 GiB 计算：428.3/2000 ≈ 21%
	if !strings.Contains(plain, "21%") {
		t.Errorf("百分比计算异常:\n%s", text)
	}
	// 结构断言：标题与标签必须是粗体，IP 必须是等宽
	for _, want := range []string{"*状态：*", "*流量：*", "`38.105.28.165`", "*主 IP：*"} {
		if !strings.Contains(text, want) {
			t.Errorf("MarkdownV2 结构缺少 %q:\n%s", want, text)
		}
	}
	// 动态文本里的保留字符必须被转义，否则整条消息会被 Telegram 拒收
	if strings.Contains(plain, "vps-gia-185197") && !strings.Contains(text, `vps\-gia\-185197`) {
		t.Errorf("服务名里的连字符未转义:\n%s", text)
	}
}

func TestRenderServiceDetailUnlimited(t *testing.T) {
	detail := breacloud.ServiceDetail{Service: breacloud.Service{ID: 1, DomainName: "x", Status: "active"}}
	text := RenderServiceDetail(detail, breacloud.Traffic{Unlimited: true, InBytes: 1024}, nil)
	if !strings.Contains(md.Unescape(text), "不限量") {
		t.Errorf("不限量服务渲染异常:\n%s", text)
	}
	if !strings.Contains(md.Unescape(text), "暂无缓存") {
		t.Errorf("无缓存时应提示:\n%s", text)
	}
}

func TestTruncateKeepsMessagesUnderLimit(t *testing.T) {
	short := "hello"
	if Truncate(short) != short {
		t.Error("短消息不该被改动")
	}

	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("这是一行足够长的中文内容用来把消息撑到上限之外\n")
	}
	got := Truncate(b.String())
	if len(got) > maxMessageChars {
		t.Errorf("截断后仍超长: %d 字节", len(got))
	}
	if !strings.Contains(got, "未显示") {
		t.Errorf("截断应说明还有多少未显示:\n%s", got[max(0, len(got)-100):])
	}
}

func TestPowerActionLookup(t *testing.T) {
	def, ok := powerAction("cold_reboot")
	if !ok || def.Label != "冷重启" {
		t.Fatalf("cold_reboot = %+v, %v", def, ok)
	}
	if !strings.Contains(def.Warning, "断电") {
		t.Errorf("确认文案应说明后果: %q", def.Warning)
	}
	if _, ok := powerAction("rm -rf"); ok {
		t.Error("未定义的动作不该被接受")
	}
	// 每个动作都必须能在界面上展示
	for _, a := range powerActions {
		if a.Label == "" || a.Warning == "" {
			t.Errorf("动作 %s 缺少文案", a.Action)
		}
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in      string
		wantCmd string
		wantArg string
	}{
		{"/start", "start", ""},
		{"/START", "start", ""},
		{"/allow 12345", "allow", "12345"},
		{"/allow@my_bot 42", "allow", "42"},
		{"  /menu  ", "menu", ""},
		{"普通文本", "", ""},
		{"/", "", ""},
	}
	for _, tc := range cases {
		cmd, args := parseCommand(tc.in)
		if cmd != tc.wantCmd {
			t.Errorf("parseCommand(%q) cmd = %q, want %q", tc.in, cmd, tc.wantCmd)
		}
		if tc.wantArg != "" && (len(args) == 0 || args[0] != tc.wantArg) {
			t.Errorf("parseCommand(%q) args = %v", tc.in, args)
		}
	}
}

func TestParseChatID(t *testing.T) {
	if id, err := parseChatID("12345"); err != nil || id != 12345 {
		t.Errorf("parseChatID = %d, %v", id, err)
	}
	if id, err := parseChatID("-100123"); err != nil || id != -100123 {
		t.Errorf("负 id 应被接受: %d, %v", id, err)
	}
	for _, bad := range []string{"abc", "", "0"} {
		if _, err := parseChatID(bad); err == nil {
			t.Errorf("parseChatID(%q) 应报错", bad)
		}
	}
}

func TestShiftClockKeepsFormat(t *testing.T) {
	got := shiftClock("bogus", reportTimeStep)
	if got != "09:30" {
		t.Errorf("非法时间应先回退默认值: %q", got)
	}
	if _, err := time.Parse("15:04", got); err != nil {
		t.Errorf("输出格式不是 HH:MM: %q", got)
	}
}

func TestUnauthorizedHintCarriesChatID(t *testing.T) {
	hint := UnauthorizedHint(-1001234567890)
	if !strings.Contains(hint, "-1001234567890") {
		t.Errorf("提示里必须带上 chat id，否则用户没法自助：%q", hint)
	}
	if strings.Count(hint, "-1001234567890") < 2 {
		t.Errorf("应同时给出可直接复制执行的 /allow 命令：%q", hint)
	}
}
