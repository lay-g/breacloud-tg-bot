package bot

import (
	"fmt"

	"github.com/go-telegram/bot/models"
	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/humanize"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// kb 把按钮行组装成 InlineKeyboardMarkup。
func kb(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func btn(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

// kbMainMenu 是主菜单键盘。
func kbMainMenu() *models.InlineKeyboardMarkup {
	return kb(
		[]models.InlineKeyboardButton{
			btn("🖥 VPS 列表", cbMenu(pageVPS)),
			btn("📊 昨日报告", cbMenu(pageReport)),
		},
		[]models.InlineKeyboardButton{
			btn("⚙️ 设置", cbMenu(pageSettings)),
			btn("❓ 帮助", cbMenu(pageHelp)),
		},
	)
}

// kbBackToMenu 是只有一颗返回按钮的键盘。
func kbBackToMenu() *models.InlineKeyboardMarkup {
	return kb([]models.InlineKeyboardButton{btn("⬅️ 主菜单", cbMainMenu())})
}

// kbRegionList 渲染区域列表的分页键盘。
func kbRegionList(regions []Region, page, pages int) *models.InlineKeyboardMarkup {
	start, end := sliceBounds(page, regionPerPage, len(regions))
	rows := make([][]models.InlineKeyboardButton, 0, pages+2)
	for i := start; i < end; i++ {
		// 回调里用下标而不是区域名：区域名可能超出 64 字节的限制
		rows = append(rows, []models.InlineKeyboardButton{
			btn(fmt.Sprintf("%s（%d）", regions[i].Name, len(regions[i].Services)), cbRegionAt(page, i)),
		})
	}
	if pager := pagerRow(page, pages, cbRegionAt); len(pager) > 0 {
		rows = append(rows, pager)
	}
	rows = append(rows, []models.InlineKeyboardButton{btn("⬅️ 主菜单", cbMainMenu())})
	return kb(rows...)
}

// kbRegionServices 渲染某区域内 VPS 列表的键盘。
func kbRegionServices(services []breacloud.Service, regionPage, index, page, pages int) *models.InlineKeyboardMarkup {
	start, end := sliceBounds(page, servicePerPage, len(services))
	rows := make([][]models.InlineKeyboardButton, 0, servicePerPage+2)
	for i := start; i < end; i++ {
		s := services[i]
		rows = append(rows, []models.InlineKeyboardButton{
			btn(fmt.Sprintf("%s %s", statusIcon(s.Status), s.DisplayName()), cbService(s.ID)),
		})
	}
	if pager := pagerRow(page, pages, func(p, _ int) string { return cbPageAt(p, index) }); len(pager) > 0 {
		rows = append(rows, pager)
	}
	rows = append(rows, []models.InlineKeyboardButton{
		btn("⬅️ 返回区域", cbRegionAt(regionPage, index)),
		btn("🏠 主菜单", cbMainMenu()),
	})
	return kb(rows...)
}

// kbServiceDetail 是单台 VPS 的操作键盘。
func kbServiceDetail(serviceID int64) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(powerActions)+3)
	for i := 0; i < len(powerActions); i += 2 {
		row := []models.InlineKeyboardButton{
			btn(powerActions[i].Label, cbPowerAction(serviceID, powerActions[i].Action)),
		}
		if i+1 < len(powerActions) {
			row = append(row, btn(powerActions[i+1].Label, cbPowerAction(serviceID, powerActions[i+1].Action)))
		}
		rows = append(rows, row)
	}
	rows = append(rows,
		[]models.InlineKeyboardButton{
			btn("🔄 刷新用量", cbRefreshService(serviceID)),
			btn("🧾 查看任务", cbServiceTasks(serviceID)),
		},
		[]models.InlineKeyboardButton{btn("🏠 主菜单", cbMainMenu())},
	)
	return kb(rows...)
}

// kbPowerConfirm 是电源操作的二次确认键盘。
func kbPowerConfirm(serviceID int64, action string) *models.InlineKeyboardMarkup {
	return kb(
		[]models.InlineKeyboardButton{
			btn("✅ 确认执行", cbPowerConfirm(serviceID, action)),
			btn("❌ 取消", cbService(serviceID)),
		},
	)
}

// kbTasks 是任务页的返回键盘。
func kbTasks(serviceID int64) *models.InlineKeyboardMarkup {
	return kb([]models.InlineKeyboardButton{
		btn("⬅️ 返回", cbService(serviceID)),
		btn("🏠 主菜单", cbMainMenu()),
	})
}

// kbSettings 是设置面板键盘。所有设置项都只用按钮修改，不引入文本输入状态机。
func kbSettings(s store.Settings) *models.InlineKeyboardMarkup {
	return kb(
		[]models.InlineKeyboardButton{
			btn(fmt.Sprintf("每日报告：%s", humanize.OnOff(s.ReportEnabled)), cbSetting("report", "toggle")),
		},
		[]models.InlineKeyboardButton{
			btn("➖ 30 分钟", cbSetting("report_time", "minus")),
			btn(s.ReportTime, cbSetting("noop", "-")),
			btn("➕ 30 分钟", cbSetting("report_time", "plus")),
		},
		[]models.InlineKeyboardButton{
			btn(fmt.Sprintf("流量预警：%s", humanize.OnOff(s.TrafficAlertEnabled)), cbSetting("traffic", "toggle")),
		},
		thresholdRow(s, 70, 80),
		thresholdRow(s, 90, 100),
		[]models.InlineKeyboardButton{
			btn(fmt.Sprintf("到期预警：%s", humanize.OnOff(s.ExpiryAlertEnabled)), cbSetting("expiry", "toggle")),
		},
		expiryDaysRow(s),
		[]models.InlineKeyboardButton{btn("🏠 主菜单", cbMainMenu())},
	)
}

func thresholdRow(s store.Settings, a, b int) []models.InlineKeyboardButton {
	return []models.InlineKeyboardButton{
		btn(thresholdLabel(s, a), cbSetting("threshold", fmt.Sprint(a))),
		btn(thresholdLabel(s, b), cbSetting("threshold", fmt.Sprint(b))),
	}
}

// thresholdLabel 用圆点表示该阈值是否启用。
func thresholdLabel(s store.Settings, value int) string {
	mark := "○"
	for _, t := range s.TrafficThresholds {
		if t == value {
			mark = "●"
			break
		}
	}
	return fmt.Sprintf("%s %d%%", mark, value)
}

func expiryDaysRow(s store.Settings) []models.InlineKeyboardButton {
	values := []int{1, 2, 3, 5, 7}
	row := make([]models.InlineKeyboardButton, 0, len(values))
	for _, v := range values {
		mark := "○"
		if s.ExpiryDays == v {
			mark = "●"
		}
		row = append(row, btn(fmt.Sprintf("%s %d天", mark, v), cbSetting("days", fmt.Sprint(v))))
	}
	return row
}

// pagerRow 构造上一页/下一页按钮，只有一页时返回 nil。
func pagerRow(page, pages int, makeData func(page, index int) string) []models.InlineKeyboardButton {
	if pages <= 1 {
		return nil
	}
	row := make([]models.InlineKeyboardButton, 0, 2)
	if page > 0 {
		row = append(row, btn("⬅️ 上一页", makeData(page-1, 0)))
	}
	if page < pages-1 {
		row = append(row, btn("下一页 ➡️", makeData(page+1, 0)))
	}
	return row
}
