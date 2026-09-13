// Package jobs 是定时任务：每日报告、到期提醒、流量预警。
//
// 取数与聚合在这里，消息怎么发交给 notify.Notifier，因此任务逻辑可以在没有
// Telegram 的环境里完整跑一遍（serve --dry-run）。
package jobs

import (
	"log/slog"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/notify"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// bcZone 是 BreaCloud 后端的本地时区，业务日期一律按它计算，不跟随主机时区。
var bcZone = time.FixedZone("UTC+8", 8*3600)

// JobDaily 是每日任务的幂等标记名。
const JobDaily = "daily"

// 取数并发与保留窗口。
const (
	fetchConcurrency = 5
	usageRetention   = 400 * 24 * time.Hour
	jobRunRetention  = 90 * 24 * time.Hour
)

// Deps 是任务需要的全部依赖。
type Deps struct {
	Store  *store.Store
	API    *breacloud.Client
	Cache  *breacloud.ServiceCache
	Notify notify.Notifier
	Log    *slog.Logger
	// Now 可替换，便于测试固定「今天是哪天」。
	Now func() time.Time
}

// now 返回当前时间。
func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// log 返回日志器，未注入时用默认的。
func (d Deps) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}

// zoneNow 返回 UTC+8 时区的当前时间。
func (d Deps) zoneNow() time.Time {
	return d.now().In(bcZone)
}

// localDay 返回主机本地日期，用于每日任务的幂等标记。
func (d Deps) localDay() string {
	return d.now().Format("2006-01-02")
}
