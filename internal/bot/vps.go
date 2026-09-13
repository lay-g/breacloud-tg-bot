package bot

import (
	"context"
	"fmt"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// bcZone 是 BreaCloud 后端的本地时区。业务日期一律按它计算，不跟随主机时区。
var bcZone = time.FixedZone("UTC+8", 8*3600)

// showRegions 展示按区域分组的列表。
func (b *Bot) showRegions(ctx context.Context, tg *tgbot.Bot, chatID int64, page int) {
	services, err := b.cache.List(ctx, false)
	if err != nil && len(services) == 0 {
		b.send(ctx, tg, chatID, "获取 VPS 列表失败："+err.Error(), kbBackToMenu())
		return
	}
	regions := GroupByRegion(services)
	text, pages := RenderRegionList(regions, page)
	b.send(ctx, tg, chatID, text, kbRegionList(regions, clampPage(page, pages), pages))
}

// showRegionServices 展示某个区域内的 VPS 列表。
//
// index 是 GroupByRegion 结果里的下标。列表每次重新计算并按同一规则排序，
// 下标越界说明列表变了，直接提示用户重新打开。
func (b *Bot) showRegionServices(ctx context.Context, tg *tgbot.Bot, chatID int64, regionPage, index, page int) {
	services, err := b.cache.List(ctx, false)
	if err != nil && len(services) == 0 {
		b.send(ctx, tg, chatID, "获取 VPS 列表失败："+err.Error(), kbBackToMenu())
		return
	}
	regions := GroupByRegion(services)
	if index < 0 || index >= len(regions) {
		b.send(ctx, tg, chatID, "列表已更新，请重新打开。", kbBackToMenu())
		return
	}
	region := regions[index]
	text, pages := RenderRegionServices(region.Services, page)
	page = clampPage(page, pages)
	title := fmt.Sprintf("%s · %d 台\n\n", region.Name, len(region.Services)) + text
	if regionPage < 0 {
		// 翻页时不知道来源页，用当前页兜底
		regionPage = 0
	}
	b.send(ctx, tg, chatID, title, kbRegionServices(region.Services, regionPage, index, page, pages))
}

// showService 展示单台 VPS 详情。forceRefresh 为真时先拉一次最新日用量。
func (b *Bot) showService(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, serviceID int64, forceRefresh bool) {
	detail, err := b.api.GetService(ctx, serviceID)
	if err != nil {
		b.edit(ctx, tg, chatID, messageID, "获取详情失败："+err.Error(), kbBackToMenu())
		return
	}
	if forceRefresh {
		if err := b.refreshUsage(ctx, serviceID); err != nil {
			b.log.Warn("刷新用量失败", "service_id", serviceID, "error", err)
		}
	}
	traffic, err := b.api.GetTraffic(ctx, serviceID)
	if err != nil {
		// 流量拿不到不该挡住电源操作，用空值继续渲染
		b.log.Warn("获取流量失败", "service_id", serviceID, "error", err)
	}
	days := b.recentUsage(ctx, serviceID)
	b.edit(ctx, tg, chatID, messageID, RenderServiceDetail(detail, traffic, days), kbServiceDetail(serviceID))
}

// refreshUsage 拉取最近 8 天日桶并写入本地缓存。
func (b *Bot) refreshUsage(ctx context.Context, serviceID int64) error {
	history, err := b.api.GetTrafficWeek(ctx, serviceID)
	if err != nil {
		return err
	}
	return b.store.SaveDailyUsage(ctx, serviceID, historyToUsage(history, b.now()))
}

// recentUsage 读取本地缓存的近 7 天用量，按日期倒序。
func (b *Bot) recentUsage(ctx context.Context, serviceID int64) []store.DailyUsage {
	to := b.now().In(bcZone).Format("2006-01-02")
	from := b.now().In(bcZone).AddDate(0, 0, -usageDaysShown+1).Format("2006-01-02")
	days, err := b.store.DailyUsage(ctx, serviceID, from, to)
	if err != nil {
		b.log.Warn("读取日用量失败", "service_id", serviceID, "error", err)
		return nil
	}
	return days
}

// historyToUsage 把接口返回的日桶转成可写入缓存的结构。
func historyToUsage(history breacloud.History, now time.Time) []store.DailyUsage {
	out := make([]store.DailyUsage, 0, len(history.Daily))
	for _, d := range history.Daily {
		out = append(out, store.DailyUsage{
			Day:       d.Bucket,
			InBytes:   d.InBytes,
			OutBytes:  d.OutBytes,
			FetchedAt: now,
		})
	}
	return out
}

// confirmPowerAction 渲染二次确认。
func (b *Bot) confirmPowerAction(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, serviceID int64, action string) {
	def, ok := powerAction(action)
	if !ok {
		b.alert(ctx, tg, chatID, "不支持的操作。")
		return
	}
	name := fmt.Sprintf("服务 #%d", serviceID)
	if detail, err := b.api.GetService(ctx, serviceID); err == nil {
		name = detail.Service.DisplayName()
	}
	b.edit(ctx, tg, chatID, messageID, RenderPowerConfirm(name, def), kbPowerConfirm(serviceID, action))
}

// runPowerAction 执行电源操作。只有走到这里才会真正调用接口，且不做自动重试。
func (b *Bot) runPowerAction(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, serviceID int64, action string) {
	def, ok := powerAction(action)
	if !ok {
		b.alert(ctx, tg, chatID, "不支持的操作。")
		return
	}
	if err := b.api.DoAction(ctx, serviceID, action); err != nil {
		b.edit(ctx, tg, chatID, messageID, fmt.Sprintf("❌ %s失败\n\n%s", def.Label, err.Error()), kbServiceDetail(serviceID))
		return
	}
	text := fmt.Sprintf("✅ 已下发：%s\n\n服务 #%d\n\n操作是异步执行的，点「查看任务」可以看进度。", def.Label, serviceID)
	b.edit(ctx, tg, chatID, messageID, text, kbServiceDetail(serviceID))
	b.log.Info("已下发电源操作", "service_id", serviceID, "action", action, "chat_id", chatID)
}

// showTasks 展示某台服务的最近任务。
func (b *Bot) showTasks(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, serviceID int64) {
	tasks, err := b.api.ListTasks(ctx, serviceID)
	if err != nil {
		b.edit(ctx, tg, chatID, messageID, "获取任务失败："+err.Error(), kbTasks(serviceID))
		return
	}
	b.edit(ctx, tg, chatID, messageID, RenderTasks(tasks), kbTasks(serviceID))
}
