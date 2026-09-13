package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
)

// ReportFunc 生成一份即时报告文本。由 serve 注入 jobs 的实现，避免 bot 直接依赖 jobs。
type ReportFunc func(ctx context.Context) (string, error)

// Bot 是 Telegram 交互层。
type Bot struct {
	cfg   *config.Config
	store *store.Store
	api   *breacloud.Client
	cache *breacloud.ServiceCache
	log   *slog.Logger
	tg    *tgbot.Bot

	now      func() time.Time
	reportFn ReportFunc
}

// New 构造 Bot。cache 应当与定时任务共用同一个实例。
func New(cfg *config.Config, st *store.Store, api *breacloud.Client, cache *breacloud.ServiceCache) (*Bot, error) {
	if cache == nil {
		cache = breacloud.NewServiceCache(api, 0)
	}
	b := &Bot{
		cfg:   cfg,
		store: st,
		api:   api,
		cache: cache,
		log:   slog.Default(),
		now:   time.Now,
	}
	tg, err := tgbot.New(cfg.Telegram.BotToken,
		tgbot.WithDefaultHandler(b.onUpdate),
		tgbot.WithErrorsHandler(func(err error) {
			b.log.Error("telegram 调用出错", "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 Telegram 客户端: %w", err)
	}
	b.tg = tg
	return b, nil
}

// SetReportFunc 注入即时报告生成函数（/report 与菜单按钮使用）。
func (b *Bot) SetReportFunc(fn ReportFunc) {
	b.reportFn = fn
}

// Run 注册菜单并开始长轮询，阻塞直到 ctx 结束。
func (b *Bot) Run(ctx context.Context) error {
	if err := b.registerCommands(ctx); err != nil {
		// 菜单注册失败不该拦住服务：命令本身依然可用，只是客户端不显示提示。
		b.log.Warn("注册命令菜单失败", "error", err)
	}
	b.log.Info("Telegram 长轮询已启动")
	b.tg.Start(ctx)
	return nil
}

// Send 发给单个 chat，实现 notify.Notifier。
func (b *Bot) Send(ctx context.Context, chatID int64, text string) error {
	_, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: chatID,
		Text:   Truncate(text),
	})
	return err
}

// Broadcast 发给白名单内全部 chat，实现 notify.Notifier。
//
// 单个 chat 失败不影响其它 chat，最后汇总错误。
func (b *Bot) Broadcast(ctx context.Context, text string) error {
	chats, err := b.store.Chats(ctx)
	if err != nil {
		return err
	}
	var failed int
	for _, chatID := range chats {
		if err := b.Send(ctx, chatID, text); err != nil {
			failed++
			b.log.Warn("广播失败", "chat_id", chatID, "error", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("广播失败 %d/%d 个会话", failed, len(chats))
	}
	return nil
}

// registerCommands 注册客户端输入框里的命令菜单。
//
// 每次启动都重新注册，因此改了命令表重启即可生效；管理命令不进菜单，只在 /help 里列出。
func (b *Bot) registerCommands(ctx context.Context) error {
	_, err := b.tg.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "menu", Description: "主菜单"},
			{Command: "vps", Description: "按区域查看 VPS"},
			{Command: "report", Description: "查看昨日流量报告"},
			{Command: "settings", Description: "设置报告与预警"},
			{Command: "help", Description: "帮助"},
			{Command: "start", Description: "开始使用"},
		},
	})
	return err
}

// onUpdate 是唯一入口，按更新类型分发。
func (b *Bot) onUpdate(ctx context.Context, tg *tgbot.Bot, update *models.Update) {
	switch {
	case update.Message != nil:
		b.onMessage(ctx, tg, update.Message)
	case update.CallbackQuery != nil:
		b.onCallback(ctx, tg, update.CallbackQuery)
	}
}

// onMessage 处理文本消息。只响应私聊：需求明确不支持群聊。
func (b *Bot) onMessage(ctx context.Context, tg *tgbot.Bot, msg *models.Message) {
	if msg.Chat.Type != models.ChatTypePrivate {
		return
	}
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)
	cmd, args := parseCommand(text)

	if !b.authorize(ctx, tg, chatID) {
		// 未授权时只对 /start 与 /menu 回一次带自身 chat id 的提示，
		// 其它消息静默忽略，避免任何人用这个 Bot 探测它是否存在。
		if cmd == "start" || cmd == "menu" {
			b.send(ctx, tg, chatID, UnauthorizedHint(chatID), nil)
		}
		return
	}

	switch cmd {
	case "start":
		b.send(ctx, tg, chatID, RenderWelcome()+"\n\n"+RenderMainMenu(), kbMainMenu())
	case "menu":
		b.send(ctx, tg, chatID, RenderMainMenu(), kbMainMenu())
	case "help":
		b.send(ctx, tg, chatID, RenderHelp(), kbBackToMenu())
	case "vps":
		b.showRegions(ctx, tg, chatID, 0)
	case "report":
		b.showReport(ctx, tg, chatID)
	case "settings":
		b.showSettings(ctx, tg, chatID)
	case "allow":
		b.handleAllow(ctx, tg, chatID, args)
	case "deny":
		b.handleDeny(ctx, tg, chatID, args)
	case "allowlist":
		b.handleAllowlist(ctx, tg, chatID)
	case "":
		// 非命令文本：给一次菜单，避免用户以为机器人没反应
		b.send(ctx, tg, chatID, RenderMainMenu(), kbMainMenu())
	default:
		b.send(ctx, tg, chatID, "未知命令，发送 /help 查看可用命令。", kbBackToMenu())
	}
}

// onCallback 处理按钮回调。
func (b *Bot) onCallback(ctx context.Context, tg *tgbot.Bot, query *models.CallbackQuery) {
	if query.Message.Type != models.MaybeInaccessibleMessageTypeMessage || query.Message.Message == nil {
		// 消息太旧被清理掉了，只能应答一下
		_, _ = tg.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
		return
	}
	msg := query.Message.Message
	chatID := msg.Chat.ID

	if !b.authorize(ctx, tg, chatID) {
		_, _ = tg.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "未授权",
			ShowAlert:       true,
		})
		return
	}

	cb, ok := ParseCallback(query.Data)
	if !ok {
		_, _ = tg.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: query.ID})
		return
	}

	// 先应答，避免客户端转圈；需要提示时再用 ShowAlert 单独发一条。
	_, _ = tg.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: query.ID})

	b.dispatchCallback(ctx, tg, chatID, msg.ID, cb)
}

// authorize 检查访问权限，未授权时对 /start 给一次带 chat id 的提示，其它消息静默忽略。
//
// 静默是刻意的：否则任何人都能用这个 Bot 探测它是否存在。
func (b *Bot) authorize(ctx context.Context, tg *tgbot.Bot, chatID int64) bool {
	allowed, err := b.store.IsAllowed(ctx, chatID)
	if err != nil {
		b.log.Error("读取白名单失败", "chat_id", chatID, "error", err)
		return false
	}
	if !allowed {
		b.log.Info("忽略未授权的会话", "chat_id", chatID)
		return false
	}
	b.greetIfNeeded(ctx, tg, chatID)
	return true
}

// greetIfNeeded 首次通过鉴权时发送招呼与功能说明。
func (b *Bot) greetIfNeeded(ctx context.Context, tg *tgbot.Bot, chatID int64) {
	need, err := b.store.NeedGreeting(ctx, chatID)
	if err != nil || !need {
		return
	}
	if err := b.Send(ctx, chatID, RenderWelcome()); err != nil {
		b.log.Warn("发送欢迎消息失败", "chat_id", chatID, "error", err)
		return
	}
	if err := b.store.MarkGreeted(ctx, chatID); err != nil {
		b.log.Warn("记录欢迎状态失败", "chat_id", chatID, "error", err)
	}
}

// UnauthorizedHint 返回给未授权 chat 的提示，附带它自己的 chat id。
//
// 白名单为空时没有人能执行 /allow，因此必须同时给出命令行这条自救路径：
// 带上 chat id 重跑一次 install，服务会重启并把它播种进白名单。
func UnauthorizedHint(chatID int64) string {
	return fmt.Sprintf(`🚫 未授权

本机器人仅对白名单内的会话开放。

你的 chat id 是 %d

把它交给管理员后，任选一种方式加入白名单：

· 在已授权的会话里执行
  /allow %d

· 或在服务器上执行（服务会重启并生效）
  breacloud-tg-bot service install --chat-id %d --force`, chatID, chatID, chatID)
}

// send 发送一条新消息。
func (b *Bot) send(ctx context.Context, tg *tgbot.Bot, chatID int64, text string, markup models.ReplyMarkup) {
	params := &tgbot.SendMessageParams{ChatID: chatID, Text: Truncate(text)}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	if _, err := tg.SendMessage(ctx, params); err != nil {
		b.log.Warn("发送消息失败", "chat_id", chatID, "error", err)
	}
}

// edit 就地更新消息。失败时退回发送新消息（例如内容与原来完全相同会被 Telegram 拒绝）。
func (b *Bot) edit(ctx context.Context, tg *tgbot.Bot, chatID int64, messageID int, text string, markup models.ReplyMarkup) {
	params := &tgbot.EditMessageTextParams{ChatID: chatID, MessageID: messageID, Text: Truncate(text)}
	if markup != nil {
		params.ReplyMarkup = markup
	}
	if _, err := tg.EditMessageText(ctx, params); err != nil {
		b.log.Debug("编辑消息失败，改为发送新消息", "error", err)
		b.send(ctx, tg, chatID, text, markup)
	}
}

// alert 弹出一个一次性提示。
func (b *Bot) alert(ctx context.Context, tg *tgbot.Bot, chatID int64, text string) {
	b.send(ctx, tg, chatID, text, nil)
}

// parseCommand 从消息文本里解析命令与参数。
//
// 使用前缀匹配而不是 Command 匹配：Command 匹配依赖实体偏移，无法处理 /cmd@botname 形式。
func parseCommand(text string) (string, []string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", nil
	}
	fields := strings.Fields(text)
	cmd := strings.TrimPrefix(fields[0], "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), fields[1:]
}

// parseChatID 解析 /allow 与 /deny 的参数。
func parseChatID(arg string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil {
		return 0, errors.New("chat id 必须是整数")
	}
	if id == 0 {
		return 0, errors.New("chat id 不能为 0")
	}
	return id, nil
}
