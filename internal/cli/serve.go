package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/bot"
	"github.com/lay-g/breacloud-tg-bot/internal/breacloud"
	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/jobs"
	"github.com/lay-g/breacloud-tg-bot/internal/notify"
	"github.com/lay-g/breacloud-tg-bot/internal/store"
	"github.com/lay-g/breacloud-tg-bot/internal/version"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var dryRun, runNow bool
	var trafficInterval time.Duration
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "在前台启动服务",
		Long: "在前台启动 Bot：接收 Telegram 指令、执行定时任务。\n" +
			"使用 --dry-run 可把通知内容打到标准输出而不真正发送，" +
			"--run-now 会立即执行一次每日任务后退出，二者都便于在无 Telegram 环境下验证。",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveConfigPath()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runServe(ctx, cfg, serveOptions{
				dryRun:          dryRun,
				runNow:          runNow,
				trafficInterval: trafficInterval,
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "不发送 Telegram 消息，把通知打印到标准输出")
	cmd.Flags().BoolVar(&runNow, "run-now", false, "立即执行一次每日任务后退出")
	cmd.Flags().DurationVar(&trafficInterval, "traffic-interval", jobs.DefaultTrafficInterval, "流量预警检查间隔")
	return cmd
}

type serveOptions struct {
	dryRun          bool
	runNow          bool
	trafficInterval time.Duration
}

func runServe(ctx context.Context, cfg *config.Config, opts serveOptions) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	lv, err := parseLogLevel(cfg.Log.Level)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
	slog.SetDefault(logger)

	fmt.Printf("breacloud-tg-bot %s\n", version.Version)
	if cfg.FromFile() {
		fmt.Printf("  配置文件: %s\n", cfg.Path)
	} else {
		fmt.Printf("  配置来源: 环境变量（%s 不存在）\n", cfg.Path)
	}
	fmt.Printf("  API 地址: %s\n", cfg.BreaCloud.BaseURL)
	fmt.Printf("  数据库:   %s\n", cfg.Database.Path)
	fmt.Printf("  dry-run:  %v\n", opts.dryRun)

	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// 仅在白名单为空时用配置里的 owner 兜底，避免用户主动移除后被重启加回来。
	if err := st.SeedOwnerIfEmpty(ctx, cfg.Telegram.OwnerChatID); err != nil {
		return err
	}
	chats, err := st.Chats(ctx)
	if err != nil {
		return err
	}

	api := breacloud.New(cfg.BreaCloud)
	cache := breacloud.NewServiceCache(api, breacloud.DefaultCacheTTL)

	// dry-run 时完全不创建 Telegram 客户端：通知打到 stdout，交互命令不可用。
	var runner *bot.Bot
	var notifier notify.Notifier = &notify.DryRun{Chats: st.Chats, Out: os.Stdout}
	if !opts.dryRun {
		runner, err = bot.New(cfg, st, api, cache)
		if err != nil {
			return err
		}
		notifier = runner
	}

	logger.Info("服务启动",
		"version", version.Version,
		"dry_run", opts.dryRun,
		"config_from_file", cfg.FromFile(),
		"allowlist", len(chats),
		"traffic_interval", opts.trafficInterval.String())

	deps := jobs.Deps{
		Store:  st,
		API:    api,
		Cache:  cache,
		Notify: notifier,
		Log:    logger,
	}
	if runner != nil {
		// /report 与定时任务共用同一个函数，保证口径一致。
		runner.SetReportFunc(func(ctx context.Context) (string, error) {
			return jobs.BuildDailyReport(ctx, deps)
		})
	}

	if opts.runNow {
		return runDailyNow(ctx, deps)
	}

	scheduler := jobs.NewScheduler(deps, opts.trafficInterval)
	go func() {
		if err := scheduler.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("调度器退出", "error", err)
		}
	}()

	if runner == nil {
		// dry-run 没有 Telegram 轮询，靠信号退出。
		<-ctx.Done()
		logger.Info("收到退出信号，服务停止")
		return nil
	}

	err = runner.Run(ctx)
	logger.Info("服务停止")
	return err
}

// runDailyNow 立即执行一次每日任务并退出，供 --run-now 使用。
//
// 报告统一由 notifier 输出：dry-run 打印到 stdout，否则真的发给白名单。
func runDailyNow(ctx context.Context, deps jobs.Deps) error {
	text, err := jobs.BuildDailyReport(ctx, deps)
	if err != nil {
		return err
	}
	return deps.Notify.Broadcast(ctx, text)
}
