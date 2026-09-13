package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/bot"
	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/jobs"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// 本文件用真实 Telegram Bot API 校验渲染出来的 MarkdownV2。
//
// 原理：Telegram 先解析实体、再检查会话是否存在。因此把消息发到一个不存在的
// chat_id，合法语法会返回 "chat not found"，非法语法会返回
// "can't parse entities: ..."。后者正是我们要拦下的——它意味着整条消息
// 在用户那里根本发不出去。
//
// 运行：
//
//	TELEGRAM_TEST_TOKEN=123:abc go test -run Integration .
//
// 未设置环境变量时跳过。全程只向不存在的 chat 发送，不会打扰任何人。

const probeChatID = "1"

// telegramToken 只认环境变量，理由同 internal/breacloud 的集成测试：
// 不能让默认的 `go test ./...` 因为本机装了配置就联网。
func telegramToken() string {
	return os.Getenv("TELEGRAM_TEST_TOKEN")
}

// probe 发送一条消息并返回 Telegram 的错误描述。
func probe(t *testing.T, token, text string) string {
	t.Helper()
	body := url.Values{"chat_id": {probeChatID}, "text": {text}, "parse_mode": {"MarkdownV2"}}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.PostForm(fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token), body)
	if err != nil {
		t.Fatalf("请求 Telegram 失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload.OK {
		t.Fatalf("不应该真的发出去成功（chat_id=%s 是无效会话）", probeChatID)
	}
	return payload.Description
}

// 转义器本身必须真的能拦住非法语法，否则这整套测试都是自欺欺人。
func TestIntegrationProbeDetectsInvalidMarkdown(t *testing.T) {
	token := telegramToken()
	if token == "" {
		t.Skip("未设置 TELEGRAM_TEST_TOKEN，跳过真实 API 校验")
	}
	if desc := probe(t, token, "*未闭合的粗体"); !strings.Contains(desc, "can't parse entities") {
		t.Fatalf("校验器不可信：未闭合的实体没有被识别为非法（%s）", desc)
	}
	if desc := probe(t, token, `1\. 已转义的点号`); strings.Contains(desc, "can't parse entities") {
		t.Fatalf("校验器误报：合法内容被判定为非法（%s）", desc)
	}
}

func TestIntegrationRenderedMessagesAreValidMarkdown(t *testing.T) {
	token := telegramToken()
	if token == "" {
		t.Skip("未设置 TELEGRAM_TEST_TOKEN，跳过真实 API 校验")
	}

	for name, text := range renderedCorpus() {
		text := text
		t.Run(name, func(t *testing.T) {
			if text == "" {
				t.Fatal("渲染结果为空白")
			}
			desc := probe(t, token, bot.Truncate(text))
			if strings.Contains(desc, "can't parse entities") {
				t.Errorf("Telegram 拒收该消息：%s\n\n%s", desc, text)
			}
			if !strings.Contains(desc, "chat not found") {
				t.Errorf("期望 chat not found，实际：%s", desc)
			}
		})
	}
}

// renderedCorpus 覆盖所有会发给用户的文案，输入数据里塞满 MarkdownV2 保留字符。
func renderedCorpus() map[string]string {
	// 服务名、区域名、系统名里塞满保留字符：这是最容易漏转义的地方
	nasty := breacloud.Service{
		ID:            3600,
		DomainName:    "vps_gia-185197 (prod) [1].v2!",
		Status:        "active",
		RegionName:    "Los Angeles (US) [GIA]",
		OSName:        "Debian 12.5 x86_64 (64-bit)",
		PrimaryIP:     "38.105.28.165",
		BandwidthMbps: 600,
		NextDueDate:   "2027-08-23T00:00:00Z",
		BillingMode:   "periodic",
		AutoRenew:     true,
	}
	other := breacloud.Service{
		ID: 42, DomainName: "a-b_c.d", Status: "suspended", RegionName: "洛杉矶 Pro #1+", PrimaryIP: "1.2.3.4",
	}
	services := []breacloud.Service{nasty, other}

	detail := breacloud.ServiceDetail{
		Service: nasty,
		Resource: breacloud.Resource{
			VMID: 11149, NodeName: "lax-gia-main-01", PrimaryIP: "38.105.28.165",
			IPv6: "2001:db8::1", CPUCores: 1, MemoryMB: 1024, DiskGB: 20,
		},
	}
	traffic := breacloud.Traffic{QuotaGB: 2000, InBytes: 232330626871, OutBytes: 227569964859, PeriodEnd: "2026-10-01T00:00:00Z"}
	days := []store.DailyUsage{
		{Day: "2026-09-13", InBytes: 16158429527},
		{Day: "2026-09-12", InBytes: 30944777449},
	}
	settings := store.Settings{
		ReportEnabled: true, ReportTime: "09:00",
		TrafficAlertEnabled: true, TrafficThresholds: []int{80, 100},
		ExpiryAlertEnabled: true, ExpiryDays: 3,
	}

	regionText, _ := bot.RenderRegionList(bot.GroupByRegion(services), 0)
	serviceText, _ := bot.RenderRegionServices(services, 0)
	powerConfirm, ok := bot.RenderPowerConfirmFor(nasty.DomainName, "cold_reboot")
	if !ok {
		panic("cold_reboot 动作未定义")
	}

	// 长消息必须由真实渲染函数产出，才有资格检验截断后的语法
	manyServices := make([]breacloud.Service, 0, 300)
	for i := 0; i < 300; i++ {
		manyServices = append(manyServices, breacloud.Service{
			ID: int64(i), DomainName: fmt.Sprintf("vps-%03d.test", i), Status: "active", PrimaryIP: "10.0.0.1",
		})
	}
	long, _ := bot.RenderRegionServices(manyServices, 0)

	return map[string]string{
		"欢迎":         bot.RenderWelcome(),
		"帮助":         bot.RenderHelp(),
		"主菜单":        bot.RenderMainMenu(),
		"区域列表":       regionText,
		"区域详情":       serviceText,
		"VPS 详情":     bot.RenderServiceDetail(detail, traffic, days),
		"VPS 详情-不限量": bot.RenderServiceDetail(detail, breacloud.Traffic{Unlimited: true, InBytes: 1 << 30}, nil),
		"任务列表":       bot.RenderTasks([]breacloud.Task{{ID: 1, Op: "start", Label: "开机 (pxe)", Status: "ok", CreatedAt: "2026-09-13T06:30:00Z", FinishedAt: "2026-09-13T06:31:00Z"}}),
		"任务为空":       bot.RenderTasks(nil),
		"电源确认":       powerConfirm,
		"设置面板":       bot.RenderSettings(settings),
		"每日报告": jobs.RenderReport("2026-09-12", 30944777449, 26100000000, []jobs.ServiceUsage{
			{Name: nasty.DomainName, Region: nasty.RegionName, Total: 30944777449},
			{Name: other.DomainName, Total: 1024},
		}, 2),
		"报告-无数据":  jobs.RenderReport("2026-09-12", 0, 0, nil, 0),
		"流量预警":    jobs.RenderTrafficAlert([]jobs.TrafficAlert{{Name: nasty.DomainName, UsedBytes: 90 << 30, QuotaGB: 100, Percent: 90, PeriodEnd: "2026-10-01T00:00:00Z"}}),
		"到期提醒":    jobs.RenderExpiryAlert([]jobs.ExpiryItem{{Name: nasty.DomainName, DueDate: "2026-09-15", DaysLeft: 2, AutoRenew: true}, {Name: other.DomainName, DueDate: "2026-09-16", DaysLeft: 3, Canceled: true}}, 3),
		"未授权提示":   bot.UnauthorizedHint(-1001234567890),
		"截断后的长消息": long,
	}
}
