package jobs

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// topCount 是报告里展示的用量排名数量。
const topCount = 5

// sweepResult 是一次全账号日用量扫描的结果。
type sweepResult struct {
	// Day 是目标日（UTC+8 的昨天）。
	Day string
	// Totals / PrevTotals 按服务 id 记录目标日与前一日的总用量。
	Totals     map[int64]int64
	PrevTotals map[int64]int64
	// Services 是本次扫描覆盖的服务列表。
	Services []breacloud.Service
	// Failed 是请求失败的服务数量（这些机器按 0 计入总量）。
	//
	// 只统计请求失败：某台机器当天没有日桶是正常的（例如昨天才开通），
	// 把它算成失败会让报告天天出现噪音警告。
	Failed int
}

// sweepDailyUsage 并发拉取所有服务的最近 8 天日用量，写入本地缓存，并汇总目标日数据。
//
// 只发一次请求就同时满足三件事：日报需要的前一日总量、前一日对比、日用量缓存刷新。
// 这也是为什么固定用 range=week 而不是 range=day：一次请求覆盖 8 天，成本相同。
func sweepDailyUsage(ctx context.Context, d Deps) (sweepResult, error) {
	result := sweepResult{
		Day:        d.zoneNow().AddDate(0, 0, -1).Format("2006-01-02"),
		Totals:     make(map[int64]int64),
		PrevTotals: make(map[int64]int64),
	}
	prevDay := d.zoneNow().AddDate(0, 0, -2).Format("2006-01-02")

	services, err := d.Cache.List(ctx, false)
	if err != nil && len(services) == 0 {
		return sweepResult{}, err
	}
	result.Services = services
	if err != nil {
		// 有缓存兜底，继续用旧列表，但要让调用方知道刷新失败了
		d.log().Warn("服务列表刷新失败，使用缓存", "error", err)
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, fetchConcurrency)
	)

	for _, svc := range services {
		wg.Add(1)
		sem <- struct{}{}
		go func(svc breacloud.Service) {
			defer wg.Done()
			defer func() { <-sem }()

			history, err := d.API.GetTrafficWeek(ctx, svc.ID)
			if err != nil {
				d.log().Warn("拉取日用量失败", "service_id", svc.ID, "error", err)
				mu.Lock()
				result.Failed++
				mu.Unlock()
				return
			}
			// 顺便把缓存刷新到最新，失败不影响本次统计
			if err := d.Store.SaveDailyUsage(ctx, svc.ID, toStoreUsage(history, d.now())); err != nil {
				d.log().Warn("写入日用量缓存失败", "service_id", svc.ID, "error", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if bucket, ok := history.FindDay(result.Day); ok {
				result.Totals[svc.ID] = bucket.Total()
			} else {
				// 当天没有日桶：新开的机器或当天完全无流量，都按 0 计，不算失败
				result.Totals[svc.ID] = 0
			}
			if bucket, ok := history.FindDay(prevDay); ok {
				result.PrevTotals[svc.ID] = bucket.Total()
			}
		}(svc)
	}
	wg.Wait()

	return result, nil
}

// toStoreUsage 把接口返回的日桶转成可写入缓存的结构。
func toStoreUsage(history breacloud.History, now time.Time) []store.DailyUsage {
	out := make([]store.DailyUsage, 0, len(history.Daily))
	for _, b := range history.Daily {
		out = append(out, store.DailyUsage{
			Day:       b.Bucket,
			InBytes:   b.InBytes,
			OutBytes:  b.OutBytes,
			FetchedAt: now,
		})
	}
	return out
}

// summarize 把扫描结果聚合成总量、前五名与前一日总量。
func (r sweepResult) summarize() (total, prevTotal int64, top []ServiceUsage) {
	for _, t := range r.Totals {
		total += t
	}
	for _, t := range r.PrevTotals {
		prevTotal += t
	}

	usages := make([]ServiceUsage, 0, len(r.Services))
	for _, svc := range r.Services {
		usages = append(usages, ServiceUsage{
			Name:   svc.DisplayName(),
			Region: svc.RegionName,
			Total:  r.Totals[svc.ID],
		})
	}
	// 同值时按名称升序，保证每次输出一致
	sort.SliceStable(usages, func(i, j int) bool {
		if usages[i].Total != usages[j].Total {
			return usages[i].Total > usages[j].Total
		}
		return usages[i].Name < usages[j].Name
	})
	if len(usages) > topCount {
		usages = usages[:topCount]
	}
	return total, prevTotal, usages
}

// renderSweep 把扫描结果渲染成报告文案。
func renderSweep(result sweepResult) string {
	total, prevTotal, top := result.summarize()
	return RenderReport(result.Day, total, prevTotal, top, result.Failed)
}

// BuildDailyReport 生成昨日报告文案但不发送。Bot 的 /report 与定时任务共用它。
func BuildDailyReport(ctx context.Context, d Deps) (string, error) {
	result, err := sweepDailyUsage(ctx, d)
	if err != nil {
		return "", err
	}
	return renderSweep(result), nil
}

// RunDailyReport 生成并广播昨日报告。
//
// 取数部分失败但拿到列表时仍然发送：宁可发一份标注了缺失台数的报告，
// 也不要因为一台机器超时让整个报告消失。
func RunDailyReport(ctx context.Context, d Deps) error {
	result, err := sweepDailyUsage(ctx, d)
	if err != nil {
		return err
	}
	total, _, _ := result.summarize()
	d.log().Info("生成日报",
		"day", result.Day, "total_bytes", total, "services", len(result.Services), "failed", result.Failed)
	return d.Notify.Broadcast(ctx, renderSweep(result))
}
