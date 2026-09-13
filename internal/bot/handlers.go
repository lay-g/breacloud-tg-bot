package bot

import (
	"context"
	"strconv"

	tgbot "github.com/go-telegram/bot"
)

// dispatchCallback 把回调分派到具体处理函数。
func (b *Bot) dispatchCallback(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, cb Callback) {
	switch cb.Kind {
	case cbMain:
		switch cb.Arg(0) {
		case pageVPS:
			b.showRegions(ctx, tg, chatID, 0)
		case pageReport:
			b.showReport(ctx, tg, chatID)
		case pageSettings:
			b.showSettings(ctx, tg, chatID)
		case pageHelp:
			b.edit(ctx, tg, chatID, messageID, RenderHelp(), kbBackToMenu())
		default:
			b.edit(ctx, tg, chatID, messageID, RenderMainMenu(), kbMainMenu())
		}

	case cbRegion:
		page, _ := cb.ArgInt(0)
		if len(cb.Args) == 1 {
			b.showRegions(ctx, tg, chatID, int(page))
			return
		}
		index, _ := cb.ArgInt(1)
		b.showRegionServices(ctx, tg, chatID, int(page), int(index), 0)

	case cbPage:
		page, _ := cb.ArgInt(0)
		index, _ := cb.ArgInt(1)
		b.showRegionServices(ctx, tg, chatID, -1, int(index), int(page))

	case cbView:
		if id, ok := cb.ArgInt(0); ok {
			b.showService(ctx, tg, chatID, messageID, id, false)
		}

	case cbRefresh:
		if id, ok := cb.ArgInt(0); ok {
			b.showService(ctx, tg, chatID, messageID, id, true)
		}

	case cbAction:
		id, ok := cb.ArgInt(0)
		action := cb.Arg(1)
		if !ok {
			return
		}
		b.confirmPowerAction(ctx, tg, chatID, messageID, id, action)

	case cbConfirm:
		id, ok := cb.ArgInt(0)
		action := cb.Arg(1)
		if !ok {
			return
		}
		b.runPowerAction(ctx, tg, chatID, messageID, id, action)

	case cbTasks:
		if id, ok := cb.ArgInt(0); ok {
			b.showTasks(ctx, tg, chatID, messageID, id)
		}

	case cbSettings:
		b.handleSetting(ctx, tg, chatID, messageID, cb.Arg(0), cb.Arg(1))
	}
}

// showReport 立即生成并发送一份报告。用的是与定时任务完全相同的函数。
func (b *Bot) showReport(ctx context.Context, tg *tgbot.Bot, chatID int64) {
	if b.reportFn == nil {
		b.send(ctx, tg, chatID, "报告功能未启用。", kbBackToMenu())
		return
	}
	b.send(ctx, tg, chatID, "⏳ 正在汇总，机器较多时需要一会儿…", nil)
	text, err := b.reportFn(ctx)
	if err != nil {
		b.send(ctx, tg, chatID, "生成报告失败："+err.Error(), kbBackToMenu())
		return
	}
	b.send(ctx, tg, chatID, text, kbBackToMenu())
}

// handleAllow 把 chat id 加入白名单。
func (b *Bot) handleAllow(ctx context.Context, tg *tgbot.Bot, chatID int64, args []string) {
	if len(args) == 0 {
		b.send(ctx, tg, chatID, "用法：/allow <chat_id>\n\n"+
			"让目标会话给机器人发送任意消息，未授权时它不会回复；"+
			"但如果对方发送 /start，会收到一条包含自己 chat id 的提示。", kbBackToMenu())
		return
	}
	target, err := parseChatID(args[0])
	if err != nil {
		b.send(ctx, tg, chatID, "参数错误："+err.Error(), kbBackToMenu())
		return
	}
	if err := b.store.AllowChat(ctx, target); err != nil {
		b.send(ctx, tg, chatID, "添加失败："+err.Error(), kbBackToMenu())
		return
	}
	b.send(ctx, tg, chatID, "已加入白名单，该会话下次发送 /start 时会收到欢迎信息。", kbBackToMenu())
}

// handleDeny 把 chat id 移出白名单。
func (b *Bot) handleDeny(ctx context.Context, tg *tgbot.Bot, chatID int64, args []string) {
	if len(args) == 0 {
		b.send(ctx, tg, chatID, "用法：/deny <chat_id>", kbBackToMenu())
		return
	}
	target, err := parseChatID(args[0])
	if err != nil {
		b.send(ctx, tg, chatID, "参数错误："+err.Error(), kbBackToMenu())
		return
	}
	if err := b.store.DenyChat(ctx, target); err != nil {
		b.send(ctx, tg, chatID, "移除失败："+err.Error(), kbBackToMenu())
		return
	}
	b.send(ctx, tg, chatID, "已移出白名单。", kbBackToMenu())
}

// handleAllowlist 展示白名单。
func (b *Bot) handleAllowlist(ctx context.Context, tg *tgbot.Bot, chatID int64) {
	chats, err := b.store.Chats(ctx)
	if err != nil {
		b.send(ctx, tg, chatID, "读取白名单失败："+err.Error(), kbBackToMenu())
		return
	}
	if len(chats) == 0 {
		b.send(ctx, tg, chatID, "白名单为空。", kbBackToMenu())
		return
	}
	text := "👥 白名单\n"
	for _, id := range chats {
		mark := ""
		if id == chatID {
			mark = "（当前会话）"
		}
		text += "\n" + strconv.FormatInt(id, 10) + mark
	}
	b.send(ctx, tg, chatID, text, kbBackToMenu())
}
