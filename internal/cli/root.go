// Package cli 定义命令行入口与子命令。
package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/version"
	"github.com/spf13/cobra"
)

var (
	flagConfig   string
	flagLogLevel string
)

// Execute 运行根命令。
//
// 子命令会转发外部命令（如 systemctl）：失败时沿用其退出码，不再重复输出，
// 因为外部命令的诊断信息已经直接打到终端了。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "错误: "+err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   config.AppName,
		Short: "BreaCloud 的 Telegram 管理机器人",
		Long: "管理 BreaCloud 账号下的 VPS：按区域查看列表、查看每日用量、执行电源操作，" +
			"并推送每日流量报告、流量预警与到期提醒。",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true, // 由 Execute 统一打印
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return setupLogging(flagLogLevel)
		},
	}
	cmd.PersistentFlags().StringVar(&flagConfig, "config", "",
		"配置文件路径（默认 $XDG_CONFIG_HOME/breacloud-tg-bot/config.yaml）")
	cmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "",
		"日志级别：debug/info/warn/error（默认取配置文件的 log.level）")

	cmd.AddCommand(newServeCmd(), newServiceCmd(), newVersionCmd())
	return cmd
}

// parseLogLevel 把配置里的级别字符串转成 slog.Level，空值视为 info。
func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("未知日志级别 %q，可选 debug/info/warn/error", level)
	}
}

// setupLogging 按级别装配全局 slog，日志一律写 stderr，stdout 留给命令自身的输出。
func setupLogging(level string) error {
	lv, err := parseLogLevel(level)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})))
	return nil
}

// resolveConfigPath 返回生效的配置文件路径：优先 --config，其次默认路径。
func resolveConfigPath() (string, error) {
	if flagConfig != "" {
		return flagConfig, nil
	}
	return config.DefaultPath()
}
