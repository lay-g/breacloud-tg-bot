package breacloud

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
)

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// resolveTestToken 只认环境变量。
//
// 刻意不读配置文件：否则只要本机装过一次，`go test ./...` 就会默认联网跑十几秒，
// 测试变成依赖网络与账号状态的东西。需要跑时用 `make integration`。
func resolveTestToken() string {
	return os.Getenv("BREACLOUD_TEST_TOKEN")
}

// TestIntegrationRealAPI 打真实 BreaCloud API，全程只读。
//
// 运行：
//
//	BREACLOUD_TEST_TOKEN=bll_xxx go test -run Integration ./internal/breacloud/
//
// 未设置环境变量时跳过。这里绝不下发电源操作。
func TestIntegrationRealAPI(t *testing.T) {
	token := resolveTestToken()
	if token == "" {
		t.Skip("未设置 BREACLOUD_TEST_TOKEN，跳过真实 API 集成测试")
	}

	client := New(config.BreaCloudConfig{
		BaseURL:     "https://brea.cloud/api/v1",
		APIToken:    token,
		Concurrency: 3,
		Timeout:     30 * time.Second,
	})
	client.backoffBase = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	services, err := client.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	t.Logf("服务数量: %d", len(services))
	if len(services) == 0 {
		t.Skip("账号下没有服务，跳过后续断言")
	}
	var withRegion int
	for _, s := range services {
		if s.ID == 0 || s.DomainName == "" {
			t.Errorf("列表返回了不完整的服务: %+v", s)
		}
		if s.RegionName != "" {
			withRegion++
		}
	}
	if withRegion == 0 {
		t.Error("所有服务的 region_name 都为空，按区域分组会退化成一个「未分类」")
	}

	first := services[0]

	detail, err := client.GetService(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetService(%d): %v", first.ID, err)
	}
	t.Logf("详情: %s vmid=%d node=%s cpu=%d mem=%dMB disk=%dGB",
		detail.Service.DomainName, detail.Resource.VMID, detail.Resource.NodeName,
		detail.Resource.CPUCores, detail.Resource.MemoryMB, detail.Resource.DiskGB)
	if detail.Service.ID != first.ID {
		t.Errorf("详情返回的服务 id = %d, want %d", detail.Service.ID, first.ID)
	}

	// 日用量必须来自 range=week，且桶是 UTC+8 的 YYYY-MM-DD
	history, err := client.GetTrafficWeek(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetTrafficWeek(%d): %v", first.ID, err)
	}
	if history.Range != "week" {
		t.Errorf("range = %q, want week", history.Range)
	}
	if len(history.Daily) == 0 || len(history.Daily) > 8 {
		t.Errorf("日桶数量 = %d, 期望 1..8", len(history.Daily))
	}
	for _, b := range history.Daily {
		if !dayPattern.MatchString(b.Bucket) {
			t.Errorf("日桶格式异常: %q", b.Bucket)
		}
		if b.InBytes < 0 || b.OutBytes < 0 {
			t.Errorf("日桶出现负值: %+v", b)
		}
	}
	if len(history.Daily) > 1 {
		// 日桶按日期倒序，最新的一天应当是今天或前一天（跨日边界）
		now := time.Now().In(time.FixedZone("UTC+8", 8*3600))
		newest := history.Daily[0].Bucket
		if newest != now.Format("2006-01-02") && newest != now.AddDate(0, 0, -1).Format("2006-01-02") {
			t.Errorf("最新日桶 = %s, 与当前 UTC+8 日期 %s 不符", newest, now.Format("2006-01-02"))
		}
	}
	t.Logf("日桶数量: %d, 最新: %s", len(history.Daily), history.Daily[0].Bucket)

	traffic, err := client.GetTraffic(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetTraffic(%d): %v", first.ID, err)
	}
	t.Logf("周期 %s ~ %s: 配额 %d GiB, 已用 %d GiB, 字节合计 %d",
		traffic.PeriodStart, traffic.PeriodEnd, traffic.QuotaGB, traffic.UsedGB, traffic.Total())
	if traffic.Unlimited {
		t.Log("该服务流量不限量，跳过配额断言")
	} else if traffic.QuotaGB <= 0 {
		t.Errorf("非不限量服务的 quota_gb = %d", traffic.QuotaGB)
	}
	// 接口给的 used_gb 是取整过的 GiB，必须与字节换算同量级（用 10^9 换算会差约 7%）
	if !traffic.Unlimited && traffic.UsedGB > 0 {
		if got := traffic.Total() >> 30; got < traffic.UsedGB-1 || got > traffic.UsedGB {
			t.Errorf("字节换算 GiB = %d, 接口 used_gb = %d", got, traffic.UsedGB)
		}
	}

	// 任务列表是只读的，可以用来验证权限与解析
	if _, err := client.ListTasks(ctx, first.ID); err != nil {
		t.Errorf("ListTasks(%d): %v", first.ID, err)
	}
}
