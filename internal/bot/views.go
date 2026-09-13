package bot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/humanize"
	"github.com/lay-g/breacloud-tg-bot/internal/md"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// maxMessageChars 是消息长度上限。Telegram 的硬上限是 4096，留出余量。
const maxMessageChars = 3900

// regionPerPage / servicePerPage 控制列表分页大小。
const (
	regionPerPage  = 8
	servicePerPage = 10
	usageDaysShown = 7
)

// 渲染约定：本文件里的所有**动态文本**都必须过 md.Escape（或包成 md.Code），
// 静态模板文字里若出现保留字符则手写转义。另一条必须遵守的规则是
// 「任何实体都在一行内闭合」，因为 Truncate 是按行截断的。

// RenderWelcome 返回首次使用时的招呼与功能介绍。
func RenderWelcome() string {
	return strings.Join([]string{
		"👋 *欢迎使用 BreaCloud 助手*",
		"",
		"我可以帮你管理账号下的 VPS：",
		"",
		"🖥 按区域查看机器，查看状态、配置与主 IP",
		"📈 查看每台机器的每日网络用量",
		"🔌 开机关机、重启、冷重启、冷关机（会二次确认）",
		"📊 每天定时推送昨日流量报告与用量前五",
		"⚠️ 流量接近配额时主动提醒",
		"⏰ 周期机快到期时提醒",
		"",
		"发送 /menu 打开主菜单，发送 /help 查看全部命令。",
	}, "\n")
}

// RenderHelp 返回命令列表。
func RenderHelp() string {
	chatID := md.Code("<chat_id>")
	return strings.Join([]string{
		"📖 *命令列表*",
		"",
		md.Code("/menu") + "      主菜单",
		md.Code("/vps") + "       按区域查看 VPS",
		md.Code("/report") + "    立即查看昨日流量报告",
		md.Code("/settings") + "  设置（报告、流量预警、到期预警）",
		md.Code("/help") + "      本帮助",
		"",
		"*管理员命令*",
		md.Code("/allowlist") + "          查看白名单",
		md.Code("/allow") + " " + chatID + "    添加白名单",
		md.Code("/deny") + " " + chatID + "     移除白名单",
		"",
		"提示：把机器人拉进群是无效的，本机器人只响应私聊。",
	}, "\n")
}

// RenderMainMenu 返回主菜单文案。
func RenderMainMenu() string {
	return strings.Join([]string{
		"🛰 *BreaCloud 助手*",
		"",
		"请选择操作：",
	}, "\n")
}

// Region 是按区域分组后的一组服务。
type Region struct {
	Name     string
	Services []breacloud.Service
}

// GroupByRegion 按 region_name 分组，并按区域名排序。
//
// region_name 是城市级（例如 Los Angeles），来自服务列表本身，不需要额外请求。
func GroupByRegion(services []breacloud.Service) []Region {
	const uncategorized = "未分类"
	byName := make(map[string][]breacloud.Service)
	for _, s := range services {
		name := s.RegionName
		if name == "" {
			name = uncategorized
		}
		byName[name] = append(byName[name], s)
	}
	out := make([]Region, 0, len(byName))
	for name, list := range byName {
		sort.Slice(list, func(i, j int) bool {
			if list[i].DomainName != list[j].DomainName {
				return list[i].DomainName < list[j].DomainName
			}
			return list[i].ID < list[j].ID
		})
		out = append(out, Region{Name: name, Services: list})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RenderRegionList 渲染区域列表，返回文案与分页信息。
func RenderRegionList(regions []Region, page int) (string, int) {
	if len(regions) == 0 {
		return "还没有任何 VPS。", 1
	}
	pages := pageCount(len(regions), regionPerPage)
	page = clampPage(page, pages)
	start, end := sliceBounds(page, regionPerPage, len(regions))

	var b strings.Builder
	b.WriteString("🖥 *按区域查看 VPS*\n\n")
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%s · %d 台\n", md.Bold(regions[i].Name), len(regions[i].Services))
	}
	if pages > 1 {
		fmt.Fprintf(&b, "\n第 %d / %d 页", page+1, pages)
	}
	return b.String(), pages
}

// RenderRegionServices 渲染某个区域内的 VPS 列表。
func RenderRegionServices(services []breacloud.Service, page int) (string, int) {
	if len(services) == 0 {
		return "该区域下没有 VPS。", 1
	}
	pages := pageCount(len(services), servicePerPage)
	page = clampPage(page, pages)
	start, end := sliceBounds(page, servicePerPage, len(services))

	var lines []string
	for i := start; i < end; i++ {
		s := services[i]
		lines = append(lines, fmt.Sprintf("%s %s", statusIcon(s.Status), md.Bold(s.DisplayName())))
		if s.PrimaryIP != "" {
			lines = append(lines, "    "+md.Code(s.PrimaryIP))
		}
	}
	text := strings.Join(lines, "\n")
	if pages > 1 {
		text += fmt.Sprintf("\n\n第 %d / %d 页", page+1, pages)
	}
	return text, pages
}

// RenderServiceDetail 渲染单台 VPS 的详情与近 7 日用量。
func RenderServiceDetail(detail breacloud.ServiceDetail, traffic breacloud.Traffic, days []store.DailyUsage) string {
	s, r := detail.Service, detail.Resource

	lines := []string{
		fmt.Sprintf("%s %s", statusIcon(s.Status), md.Bold(s.DisplayName())),
		"",
		md.Label("状态：", statusText(s.Status)),
	}
	if r.PrimaryIP != "" {
		lines = append(lines, labelCode("主 IP：", r.PrimaryIP))
	}
	if r.IPv6 != "" {
		lines = append(lines, labelCode("IPv6：", r.IPv6))
	}
	if s.RegionName != "" {
		lines = append(lines, md.Label("区域：", s.RegionName))
	}
	if r.NodeName != "" {
		lines = append(lines, md.Label("节点：", r.NodeName))
	}
	if r.CPUCores > 0 || r.MemoryMB > 0 || r.DiskGB > 0 {
		config := fmt.Sprintf("%d 核 / %s 内存 / %d GB 磁盘", r.CPUCores, humanize.Memory(r.MemoryMB), r.DiskGB)
		lines = append(lines, md.Label("配置：", config))
	}
	if s.OSName != "" {
		lines = append(lines, md.Label("系统：", s.OSName))
	}
	if s.BandwidthMbps > 0 {
		lines = append(lines, md.Label("带宽：", fmt.Sprintf("%d Mbps", s.BandwidthMbps)))
	}
	lines = append(lines, "")

	if traffic.Unlimited {
		lines = append(lines, md.Label("流量：", "不限量，周期内已用 "+humanize.Bytes(traffic.Total())))
	} else {
		usage := fmt.Sprintf("%s / %d GB（%.0f%%）", humanize.Bytes(traffic.Total()), traffic.QuotaGB, trafficPercent(traffic))
		lines = append(lines, md.Label("流量：", usage))
		if traffic.PeriodEnd != "" {
			lines = append(lines, md.Label("计费周期至：", humanize.Date(traffic.PeriodEnd)))
		}
	}

	if len(days) > 0 {
		lines = append(lines, "", "*近 7 日用量：*")
		for _, d := range days {
			lines = append(lines, fmt.Sprintf("  %s  %s", md.Code(d.Day), md.Escape(humanize.Bytes(d.InBytes+d.OutBytes))))
		}
	} else {
		lines = append(lines, "", "*近 7 日用量：* 暂无缓存（点「刷新用量」拉取）")
	}

	if s.IsPeriodic() && s.NextDueDate != "" {
		lines = append(lines, "", md.Label("到期：", humanize.Date(s.NextDueDate)+renewSuffix(s)))
	}
	return strings.Join(lines, "\n")
}

// RenderTasks 渲染任务历史。
func RenderTasks(tasks []breacloud.Task) string {
	if len(tasks) == 0 {
		return "暂无任务记录。"
	}
	const show = 5
	if len(tasks) > show {
		tasks = tasks[:show]
	}
	lines := []string{"🧾 *最近任务*", ""}
	for _, t := range tasks {
		label := t.Label
		if label == "" {
			label = t.Op
		}
		when := "创建 " + humanize.DateTime(t.CreatedAt)
		if t.FinishedAt != "" {
			when += " · 完成 " + humanize.DateTime(t.FinishedAt)
		}
		lines = append(lines,
			fmt.Sprintf("%s %s", taskIcon(t.Status), md.Escape(label)),
			"    "+md.Escape(when),
		)
	}
	return strings.Join(lines, "\n")
}

// RenderPowerConfirm 渲染电源操作的二次确认。后果用块引用突出，避免误点。
func RenderPowerConfirm(name string, def powerActionDef) string {
	return strings.Join([]string{
		fmt.Sprintf("⚠️ %s", md.Bold(def.Label)),
		"",
		md.Quote(def.Warning),
		"",
		md.Label("目标：", name),
	}, "\n")
}

// RenderPowerConfirmFor 按动作名渲染电源确认文案。
//
// 供包外调用（构造消息与集成测试）使用：powerActionDef 不导出。
func RenderPowerConfirmFor(name, action string) (string, bool) {
	def, ok := powerAction(action)
	if !ok {
		return "", false
	}
	return RenderPowerConfirm(name, def), true
}

// RenderSettings 渲染设置面板。
func RenderSettings(s store.Settings) string {
	parts := make([]string, 0, len(s.TrafficThresholds))
	for _, t := range s.TrafficThresholds {
		parts = append(parts, fmt.Sprintf("%d%%", t))
	}
	return strings.Join([]string{
		"⚙️ *设置*",
		"",
		md.Label("每日报告：", humanize.OnOff(s.ReportEnabled)),
		md.Label("通知时间：", s.ReportTime+"（本机时区）"),
		"",
		md.Label("流量预警：", humanize.OnOff(s.TrafficAlertEnabled)),
		md.Label("预警阈值：", strings.Join(parts, "、")),
		"",
		md.Label("到期预警：", humanize.OnOff(s.ExpiryAlertEnabled)),
		md.Label("提前天数：", fmt.Sprintf("%d 天", s.ExpiryDays)),
	}, "\n")
}

// Truncate 保证消息不超过 Telegram 的长度上限。
//
// 按行截断，因此要求「任何实体都在一行内闭合」——这是本包渲染函数的硬约束，
// 否则截断会切开一个未闭合的实体，导致整条消息被 Telegram 拒收。
func Truncate(text string) string {
	if len(text) <= maxMessageChars {
		return text
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		if b.Len()+len(line)+1 > maxMessageChars-40 {
			fmt.Fprintf(&b, "\n……另有 %d 行未显示", len(lines)-i)
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------- 小工具 ----------

// labelCode 生成「加粗标签 + 等宽值」的一行，用于 IP 这类适合等宽展示的值。
func labelCode(label, raw string) string {
	return "*" + label + "* " + md.Code(raw)
}

func statusIcon(status string) string {
	switch status {
	case "active":
		return "🟢"
	case "suspended":
		return "🟡"
	case "pending":
		return "🔵"
	case "terminated", "cancelled":
		return "⚪"
	default:
		return "⚫"
	}
}

func statusText(status string) string {
	switch status {
	case "active":
		return "运行中"
	case "suspended":
		return "已暂停"
	case "pending":
		return "开通中"
	case "terminated", "cancelled":
		return "已终止"
	case "":
		return "未知"
	default:
		return status
	}
}

func taskIcon(status string) string {
	switch strings.ToLower(status) {
	case "ok", "success", "finished", "done", "completed":
		return "✅"
	case "failed", "error":
		return "❌"
	case "running", "processing":
		return "⏳"
	default:
		return "•"
	}
}

func renewSuffix(s breacloud.Service) string {
	switch {
	case s.RenewCanceledAt != "":
		return "（已关闭续费，到期释放）"
	case s.AutoRenew:
		return "（将自动续费）"
	default:
		return "（需手动续费）"
	}
}

// trafficPercent 计算已用配额百分比（按 GiB，与接口口径一致）。
func trafficPercent(t breacloud.Traffic) float64 {
	if t.QuotaGB <= 0 {
		return 0
	}
	return float64(t.Total()) / float64(t.QuotaGB<<30) * 100
}

func pageCount(total, perPage int) int {
	if total <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

func clampPage(page, pages int) int {
	if page < 0 {
		return 0
	}
	if page >= pages {
		return pages - 1
	}
	return page
}

func sliceBounds(page, perPage, total int) (int, int) {
	start := page * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return start, end
}
