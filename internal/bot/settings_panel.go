package bot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// 设置面板全部用按钮操作，不引入文本输入状态机：一旦需要记住「哪条消息在等哪个
// 字段的输入」，就要处理不回复、回复别的内容、并发多个提示等一堆分支。
//
// 可用的数值直接给成预设值：时间按 30 分钟步进，阈值 70/80/90/100 多选，
// 提前天数 1/2/3/5/7 单选。

// thresholdOptions 是界面上可选的阈值档位。
var thresholdOptions = []int{70, 80, 90, 100}

// expiryDayOptions 是界面上可选的提前天数。
var expiryDayOptions = []int{1, 2, 3, 5, 7}

// reportTimeStep 是通知时间的调整步长。
const reportTimeStep = 30 * time.Minute

// showSettings 展示设置面板。
func (b *Bot) showSettings(ctx context.Context, tg *tgbot.Bot, chatID int64) {
	settings, err := b.store.Settings(ctx)
	if err != nil {
		b.send(ctx, tg, chatID, "读取设置失败："+err.Error(), kbBackToMenu())
		return
	}
	b.send(ctx, tg, chatID, RenderSettings(settings), kbSettings(settings))
}

// handleSetting 应用一次设置修改并就地重渲染面板。
func (b *Bot) handleSetting(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, field, value string) {
	if field == "noop" {
		return
	}
	updated, err := b.store.UpdateSettings(ctx, func(s *store.Settings) {
		*s = applySetting(*s, field, value)
	})
	if err != nil {
		b.alert(ctx, tg, chatID, "保存设置失败："+err.Error())
		return
	}
	b.edit(ctx, tg, chatID, messageID, RenderSettings(updated), kbSettings(updated))
}

// applySetting 是纯函数：给定当前设置与一次操作，返回修改后的设置。
//
// 非法输入原样返回，不做部分修改——设置面板宁可无变化，也不要写进半截状态。
func applySetting(s store.Settings, field, value string) store.Settings {
	switch field {
	case "report":
		s.ReportEnabled = !s.ReportEnabled

	case "report_time":
		switch value {
		case "plus":
			s.ReportTime = shiftClock(s.ReportTime, reportTimeStep)
		case "minus":
			s.ReportTime = shiftClock(s.ReportTime, -reportTimeStep)
		}

	case "traffic":
		s.TrafficAlertEnabled = !s.TrafficAlertEnabled

	case "threshold":
		v, err := strconv.Atoi(value)
		if err != nil || !containsInt(thresholdOptions, v) {
			return s
		}
		next := toggleInt(s.TrafficThresholds, v)
		// 至少保留一个档位，否则预警等于被关掉而开关还显示为开启
		if len(next) == 0 {
			return s
		}
		s.TrafficThresholds = next

	case "expiry":
		s.ExpiryAlertEnabled = !s.ExpiryAlertEnabled

	case "days":
		v, err := strconv.Atoi(value)
		if err != nil || !containsInt(expiryDayOptions, v) {
			return s
		}
		s.ExpiryDays = v
	}
	return s
}

// shiftClock 把 HH:MM 按时长平移，跨零点回绕。
func shiftClock(clock string, delta time.Duration) string {
	t, err := time.Parse("15:04", clock)
	if err != nil {
		t, _ = time.Parse("15:04", "09:00")
	}
	base := time.Date(2000, 1, 1, t.Hour(), t.Minute(), 0, 0, time.UTC)
	shifted := base.Add(delta)
	return fmt.Sprintf("%02d:%02d", shifted.Hour(), shifted.Minute())
}

func toggleInt(values []int, v int) []int {
	out := make([]int, 0, len(values)+1)
	removed := false
	for _, x := range values {
		if x == v {
			removed = true
			continue
		}
		out = append(out, x)
	}
	if !removed {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func containsInt(values []int, v int) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
