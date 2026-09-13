package jobs

import (
	"context"
	"time"
)

// 调度周期。
const (
	// dailyTick 是检查「是否已到每日任务时间」的间隔。
	//
	// 用「已过某个时刻」而不是「等于某个时刻」判断，是为了让停机错过的任务
	// 在当天重新启动后补跑一次。
	dailyTick = 30 * time.Second
	// DefaultTrafficInterval 是流量预警的默认检查间隔。
	DefaultTrafficInterval = time.Hour
	// trafficStartDelay 是启动后首次流量检查的延迟，给服务留出就绪时间。
	trafficStartDelay = 30 * time.Second
)

// Scheduler 按时间触发三类推送。
type Scheduler struct {
	deps            Deps
	trafficInterval time.Duration
}

// NewScheduler 构造调度器。trafficInterval <= 0 时使用 DefaultTrafficInterval。
func NewScheduler(deps Deps, trafficInterval time.Duration) *Scheduler {
	if trafficInterval <= 0 {
		trafficInterval = DefaultTrafficInterval
	}
	return &Scheduler{deps: deps, trafficInterval: trafficInterval}
}

// Run 阻塞运行直到 ctx 结束。
func (s *Scheduler) Run(ctx context.Context) error {
	daily := time.NewTicker(dailyTick)
	defer daily.Stop()
	traffic := time.NewTicker(s.trafficInterval)
	defer traffic.Stop()

	// 启动后先做一次流量检查，但稍等片刻再发车，避免和启动日志抢注意力。
	// 去重状态在数据库里，重启不会造成重复推送。
	startup := time.NewTimer(trafficStartDelay)
	defer startup.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-daily.C:
			s.maybeRunDaily(ctx)
		case <-startup.C:
			s.runTraffic(ctx)
		case <-traffic.C:
			s.runTraffic(ctx)
		}
	}
}

// maybeRunDaily 在到点且当天未执行过时跑一次每日任务。
func (s *Scheduler) maybeRunDaily(ctx context.Context) {
	settings, err := s.deps.Store.Settings(ctx)
	if err != nil {
		s.deps.log().Error("读取设置失败", "error", err)
		return
	}
	// 两个开关都关掉时不执行：每日任务是唯一会全量扫描账号的路径，
	// 用户既然都关了，就不该每天打几百个请求。
	if !settings.ReportEnabled && !settings.ExpiryAlertEnabled {
		return
	}

	now := s.deps.now()
	if !dueForDaily(now, settings.ReportTime) {
		return
	}

	day := s.deps.localDay()
	ran, err := s.deps.Store.JobRan(ctx, day, JobDaily)
	if err != nil {
		s.deps.log().Error("读取任务标记失败", "error", err)
		return
	}
	if ran {
		return
	}

	s.deps.log().Info("开始执行每日任务", "day", day, "report_time", settings.ReportTime)

	result, err := sweepDailyUsage(ctx, s.deps)
	if err != nil {
		s.deps.log().Error("每日任务取数失败，本次跳过", "error", err)
		return
	}

	if settings.ReportEnabled {
		if err := s.deps.Notify.Broadcast(ctx, renderSweep(result)); err != nil {
			// 发送失败通常是 token 或网络问题，30 秒后重试只会反复失败，
			// 因此直接放弃当天，手动补发走 Bot 内的 /report。
			s.deps.log().Error("日报发送失败，当天不再重试", "error", err)
		} else {
			s.deps.log().Info("日报已发送", "day", result.Day, "services", len(result.Services))
		}
	}

	if settings.ExpiryAlertEnabled {
		items, err := RunExpiryCheck(ctx, s.deps)
		if err != nil {
			s.deps.log().Error("到期检查失败", "error", err)
		} else if err := BroadcastExpiryAlerts(ctx, s.deps, items, settings.ExpiryDays); err != nil {
			s.deps.log().Error("到期提醒发送失败", "error", err)
		} else if len(items) > 0 {
			s.deps.log().Info("到期提醒已发送", "count", len(items))
		}
	}

	s.prune(ctx)

	if err := s.deps.Store.MarkJobRan(ctx, day, JobDaily); err != nil {
		s.deps.log().Error("写入任务标记失败", "error", err)
	}
}

// runTraffic 执行一次流量检查并推送预警。
func (s *Scheduler) runTraffic(ctx context.Context) {
	alerts, err := RunTrafficCheck(ctx, s.deps)
	if err != nil {
		s.deps.log().Error("流量检查失败", "error", err)
		return
	}
	if len(alerts) == 0 {
		return
	}
	if err := BroadcastTrafficAlerts(ctx, s.deps, alerts); err != nil {
		s.deps.log().Error("流量预警发送失败", "error", err)
		return
	}
	s.deps.log().Info("流量预警已发送", "count", len(alerts))
}

// prune 清理过期的日用量与任务标记，控制数据库长期增长。
func (s *Scheduler) prune(ctx context.Context) {
	now := s.deps.now()
	if err := s.deps.Store.PruneDailyUsage(ctx, now.Add(-usageRetention).In(bcZone).Format("2006-01-02")); err != nil {
		s.deps.log().Warn("清理日用量失败", "error", err)
	}
	if err := s.deps.Store.PruneJobRuns(ctx, now.Add(-jobRunRetention).Format("2006-01-02")); err != nil {
		s.deps.log().Warn("清理任务标记失败", "error", err)
	}
}

// dueForDaily 判断当前是否已经过了设定的通知时间。
//
// 两边都是补零的 HH:MM，字典序与时间序一致，可以直接比较。
func dueForDaily(now time.Time, reportTime string) bool {
	if reportTime == "" {
		reportTime = "09:00"
	}
	return now.Format("15:04") >= reportTime
}
