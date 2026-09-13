package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/version"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var dryRun, runNow bool
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
			return runServe(ctx, cfg, dryRun, runNow)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "不发送 Telegram 消息，把通知打印到标准输出")
	cmd.Flags().BoolVar(&runNow, "run-now", false, "立即执行一次每日任务后退出")
	return cmd
}

func runServe(ctx context.Context, cfg *config.Config, dryRun, runNow bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	lv, err := parseLogLevel(cfg.Log.Level)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})))

	fmt.Printf("breacloud-tg-bot %s\n", version.Version)
	fmt.Printf("  配置文件: %s\n", cfg.Path)
	fmt.Printf("  API 地址: %s\n", cfg.BreaCloud.BaseURL)
	fmt.Printf("  数据库:   %s\n", cfg.Database.Path)
	fmt.Printf("  dry-run:  %v\n", dryRun)
	slog.Info("服务启动", "version", version.Version, "dry_run", dryRun, "run_now", runNow)

	if runNow {
		// M6 起这里调用日报任务；当前阶段仅证明配置与启动链路可用。
		fmt.Println("--run-now: 每日任务尚未接入（计划中的 M6）。")
		return nil
	}

	<-ctx.Done()
	slog.Info("收到退出信号，服务停止")
	return nil
}
