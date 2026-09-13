package jobs

import "context"

// maxAlertsPerMessage 是单条预警消息里的最大条目数。
// 超过时拆成多条，避免撞上 Telegram 的消息长度上限。
const maxAlertsPerMessage = 30

// RunTrafficCheck 检查所有服务的流量占比，返回本次需要推送的预警。
//
// 去重规则是「同一 (service, 阈值) 在比值回落到阈值以下之前只推送一次」。
// 不用计费周期做去重键：配额重置、购买流量包、周期切换都会让比值掉回阈值以下，
// 状态机会自动重新武装，因此不需要额外监听 period_start 是否变化。
func RunTrafficCheck(ctx context.Context, d Deps) ([]TrafficAlert, error) {
	settings, err := d.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.TrafficAlertEnabled {
		return nil, nil
	}

	services, err := d.Cache.List(ctx, false)
	if err != nil && len(services) == 0 {
		return nil, err
	}

	now := d.now()
	var alerts []TrafficAlert

	for _, svc := range services {
		if svc.Status == "terminated" || svc.Status == "cancelled" {
			continue
		}
		traffic, err := d.API.GetTraffic(ctx, svc.ID)
		if err != nil {
			d.log().Warn("拉取流量失败", "service_id", svc.ID, "error", err)
			continue
		}
		if traffic.Unlimited || traffic.QuotaGB <= 0 {
			continue
		}

		percent := int(traffic.Total() * 100 / traffic.QuotaBytes())
		triggered := false

		for _, threshold := range settings.TrafficThresholds {
			alertedAt, err := d.Store.AlertState(ctx, svc.ID, threshold)
			if err != nil {
				d.log().Warn("读取预警状态失败", "service_id", svc.ID, "threshold", threshold, "error", err)
				continue
			}
			switch {
			case percent >= threshold && alertedAt == nil:
				// 越过阈值且当前处于已武装状态：推送并解除武装
				at := now
				if err := d.Store.SetAlerted(ctx, svc.ID, threshold, &at); err != nil {
					d.log().Warn("写入预警状态失败", "service_id", svc.ID, "error", err)
					continue
				}
				triggered = true
			case percent < threshold && alertedAt != nil:
				// 回落到阈值以下：重新武装，允许下次再推
				if err := d.Store.SetAlerted(ctx, svc.ID, threshold, nil); err != nil {
					d.log().Warn("重新武装失败", "service_id", svc.ID, "error", err)
				}
			}
		}

		if triggered {
			alerts = append(alerts, TrafficAlert{
				Name:      svc.DisplayName(),
				UsedBytes: traffic.Total(),
				QuotaGB:   traffic.QuotaGB,
				Percent:   percent,
				PeriodEnd: traffic.PeriodEnd,
			})
		}
	}

	return alerts, nil
}

// BroadcastTrafficAlerts 把预警合并成一条消息广播；条目过多时分条发送。
func BroadcastTrafficAlerts(ctx context.Context, d Deps, alerts []TrafficAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	for start := 0; start < len(alerts); start += maxAlertsPerMessage {
		end := start + maxAlertsPerMessage
		if end > len(alerts) {
			end = len(alerts)
		}
		if err := d.Notify.Broadcast(ctx, RenderTrafficAlert(alerts[start:end])); err != nil {
			return err
		}
	}
	return nil
}
