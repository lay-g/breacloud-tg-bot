package jobs

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/humanize"
)

// RunExpiryCheck 找出即将到期的周期机。
//
// 只处理周期计费：小时机的 expire_at 不参与到期提醒（需求明确）。
// 列表为空时不发消息，由调用方决定。
func RunExpiryCheck(ctx context.Context, d Deps) ([]ExpiryItem, error) {
	settings, err := d.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.ExpiryAlertEnabled {
		return nil, nil
	}

	services, err := d.Cache.List(ctx, false)
	if err != nil && len(services) == 0 {
		return nil, err
	}

	now := d.zoneNow()
	var items []ExpiryItem

	for _, svc := range services {
		if !svc.IsPeriodic() || svc.NextDueDate == "" {
			continue
		}
		if svc.Status == "terminated" || svc.Status == "cancelled" {
			continue
		}
		due, err := time.Parse(time.RFC3339, svc.NextDueDate)
		if err != nil {
			d.log().Warn("到期时间格式无法解析", "service_id", svc.ID, "value", svc.NextDueDate)
			continue
		}
		daysLeft := int(math.Ceil(due.Sub(now).Hours() / 24))
		if daysLeft < 0 || daysLeft > settings.ExpiryDays {
			continue
		}
		items = append(items, ExpiryItem{
			Name:      svc.DisplayName(),
			DueDate:   humanize.Date(svc.NextDueDate),
			DaysLeft:  daysLeft,
			AutoRenew: svc.AutoRenew,
			Canceled:  svc.RenewCanceledAt != "",
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].DaysLeft != items[j].DaysLeft {
			return items[i].DaysLeft < items[j].DaysLeft
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// BroadcastExpiryAlerts 推送到期提醒；列表为空时不发消息。
func BroadcastExpiryAlerts(ctx context.Context, d Deps, items []ExpiryItem, days int) error {
	if len(items) == 0 {
		return nil
	}
	return d.Notify.Broadcast(ctx, RenderExpiryAlert(items, days))
}
