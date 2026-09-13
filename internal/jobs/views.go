package jobs

import (
	"fmt"
	"strings"

	"github.com/lay-g/breacloud-tg-bot/internal/humanize"
	"github.com/lay-g/breacloud-tg-bot/internal/md"
)

// 渲染约定与 internal/bot/views.go 相同：动态文本一律过 md.Escape，
// 且任何实体都在一行内闭合，因为消息可能被按行截断。

// ServiceUsage 是日报里的一行。
type ServiceUsage struct {
	Name   string
	Region string
	Total  int64
}

// RenderReport 渲染每日报告。
//
// prevTotal 为 0 时省略环比：前一日没有数据时百分比没有意义。
func RenderReport(day string, total int64, prevTotal int64, top []ServiceUsage, failed int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 *昨日流量报告* %s\n\n", md.Code(day))
	fmt.Fprintf(&b, "%s\n", md.Label("全网总量：", humanize.Bytes(total)))
	if prevTotal > 0 {
		delta := float64(total-prevTotal) / float64(prevTotal) * 100
		fmt.Fprintf(&b, "%s\n", md.Label("较前一日：", fmt.Sprintf("%+.1f%%", delta)))
	}

	if len(top) == 0 {
		b.WriteString("\n昨日没有记录到用量。")
	} else {
		b.WriteString("\n*用量前五：*\n")
		for i, u := range top {
			name := u.Name
			if u.Region != "" {
				name = fmt.Sprintf("%s（%s）", u.Name, u.Region)
			}
			fmt.Fprintf(&b, "%d\\. %s  %s\n", i+1, md.Escape(name), md.Escape(humanize.Bytes(u.Total)))
		}
	}
	if failed > 0 {
		fmt.Fprintf(&b, "\n⚠️ %d 台机器的数据获取失败，未计入统计。", failed)
	}
	return b.String()
}

// TrafficAlert 是流量预警里的一行。
type TrafficAlert struct {
	Name      string
	UsedBytes int64
	QuotaGB   int64
	Percent   int
	PeriodEnd string
}

// RenderTrafficAlert 渲染合并的流量预警。
func RenderTrafficAlert(alerts []TrafficAlert) string {
	var b strings.Builder
	b.WriteString("⚠️ *流量预警*\n")
	for _, a := range alerts {
		fmt.Fprintf(&b, "\n%s\n", md.Bold(a.Name))
		fmt.Fprintf(&b, "  已用 %s / %d GB（%d%%）",
			md.Escape(humanize.Bytes(a.UsedBytes)), a.QuotaGB, a.Percent)
		if a.PeriodEnd != "" {
			fmt.Fprintf(&b, " · 周期至 %s", md.Escape(humanize.Date(a.PeriodEnd)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ExpiryItem 是到期提醒里的一行。
type ExpiryItem struct {
	Name      string
	DueDate   string
	DaysLeft  int
	AutoRenew bool
	Canceled  bool
}

// RenderExpiryAlert 渲染合并的到期提醒。
func RenderExpiryAlert(items []ExpiryItem, days int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⏰ *到期提醒*（%d 天内）\n", days)
	for _, it := range items {
		fmt.Fprintf(&b, "\n%s\n", md.Bold(it.Name))
		fmt.Fprintf(&b, "  %s 到期（还有 %d 天）· %s",
			md.Code(it.DueDate), it.DaysLeft, md.Escape(expirySuffix(it)))
		b.WriteString("\n")
	}
	return b.String()
}

func expirySuffix(it ExpiryItem) string {
	switch {
	case it.Canceled:
		return "到期释放且不再续费"
	case it.AutoRenew:
		return "将自动续费"
	default:
		return "需手动续费"
	}
}
