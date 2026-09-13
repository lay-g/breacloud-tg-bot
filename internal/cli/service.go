package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
	"github.com/lay-g/breacloud-tg-bot/internal/systemd"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "管理 systemd 用户服务",
		Long: "把本程序注册为 systemd 用户服务（无需 sudo），支持开机自启。\n" +
			"注意：用户服务要在未登录时也持续运行，需要开启 linger，install 结束时会提示。",
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceControlCmd("start", "启动服务"),
		newServiceControlCmd("stop", "停止服务"),
		newServiceControlCmd("restart", "重启服务"),
		newServiceControlCmd("status", "查看服务状态"),
	)
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	var botToken, apiToken string
	var chatID int64
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "安装并启动 systemd 用户服务",
		Long: "写入配置与 systemd 用户单元并启动服务。\n" +
			"未通过参数提供的凭据会在终端交互询问。",
		RunE: func(*cobra.Command, []string) error {
			return runServiceInstall(botToken, apiToken, chatID, force)
		},
	}
	cmd.Flags().StringVar(&botToken, "bot-token", "", "Telegram Bot Token（不传则交互询问）")
	cmd.Flags().StringVar(&apiToken, "api-token", "", "BreaCloud API Token，bll_ 开头（不传则交互询问）")
	cmd.Flags().Int64Var(&chatID, "chat-id", 0, "首个白名单 Telegram chat id（不传则交互询问，可留空）")
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已存在的单元文件")
	return cmd
}

func runServiceInstall(botToken, apiToken string, chatID int64, force bool) error {
	if !systemd.Available() {
		return errors.New("未找到 systemctl，无法安装 systemd 用户服务")
	}
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径: %w", err)
	}
	if execPath, err = filepath.EvalSymlinks(execPath); err != nil {
		return fmt.Errorf("解析可执行文件路径: %w", err)
	}
	if isGoRunTempPath(execPath) {
		return fmt.Errorf("当前二进制是 `go run` 产生的临时文件（%s），注册成服务后会被删除。请先构建：\n"+
			"  go build -o bin/%s . && ./bin/%s service install", execPath, config.AppName, config.AppName)
	}

	cfgPath, err := resolveConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if botToken == "" {
		botToken = cfg.Telegram.BotToken
	}
	if apiToken == "" {
		apiToken = cfg.BreaCloud.APIToken
	}

	if botToken, err = askSecret("Telegram Bot Token", botToken); err != nil {
		return err
	}
	if !strings.Contains(botToken, ":") {
		return errors.New("bot token 格式不正确，应形如 123456:ABC-DEFG")
	}
	if apiToken, err = askSecret("BreaCloud API Token", apiToken); err != nil {
		return err
	}
	if !strings.HasPrefix(apiToken, "bll_") {
		return errors.New("BreaCloud API Token 格式不正确：应以 bll_ 开头")
	}
	if chatID == 0 && cfg.Telegram.OwnerChatID != 0 {
		chatID = cfg.Telegram.OwnerChatID
	}
	if chatID == 0 {
		line, err := askLine("首个白名单 chat id（可留空，之后在 Bot 内用 /allow 添加）")
		if err != nil {
			return err
		}
		if line != "" {
			chatID, err = strconv.ParseInt(line, 10, 64)
			if err != nil {
				return fmt.Errorf("chat id 必须是整数: %w", err)
			}
		}
	}

	cfg.Telegram.BotToken = botToken
	cfg.Telegram.OwnerChatID = chatID
	cfg.BreaCloud.APIToken = apiToken

	if err := writeConfigFile(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("已写入配置: %s\n", cfgPath)

	unitPath, err := config.UnitPath()
	if err != nil {
		return err
	}
	if err := systemd.Install(unitPath, systemd.UnitParams{
		ExecPath:   execPath,
		ConfigPath: cfgPath,
	}, force); err != nil {
		return err
	}
	fmt.Printf("已安装并启动服务: %s\n", systemd.ServiceName)

	if enabled, err := systemd.LingerEnabled(); err != nil {
		fmt.Printf("提示: 无法确认 linger 状态（%v），请手动检查。\n", err)
	} else if enabled {
		fmt.Println("linger 已开启：注销后服务仍会运行，并随开机自启。")
	} else {
		fmt.Println("提示: linger 未开启，注销登录后服务会被停止。要开机自启请执行：")
		fmt.Println("  sudo loginctl enable-linger $USER")
	}
	return nil
}

func newServiceUninstallCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "停止并移除 systemd 用户服务",
		Long:  "停止并移除 systemd 用户服务。默认保留配置与数据库，加 --purge 一并删除。",
		RunE: func(*cobra.Command, []string) error {
			cfgPath, err := resolveConfigPath()
			if err != nil {
				return err
			}
			unitPath, err := config.UnitPath()
			if err != nil {
				return err
			}
			var extra []string
			if purge {
				cfg, err := config.Load(cfgPath)
				if err != nil {
					return err
				}
				extra = []string{
					cfgPath,
					cfg.Database.Path,
					cfg.Database.Path + "-wal",
					cfg.Database.Path + "-shm",
				}
				fmt.Println("--purge 将删除以下文件：")
				for _, p := range extra {
					fmt.Printf("  %s\n", p)
				}
				line, err := askLine("确认删除请输入 yes")
				if err != nil {
					return err
				}
				if line != "yes" {
					return errors.New("已取消")
				}
			}
			if err := systemd.Uninstall(unitPath, purge, extra...); err != nil {
				return err
			}
			fmt.Printf("已移除服务: %s\n", systemd.ServiceName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "同时删除配置文件与数据库")
	return cmd
}

func newServiceControlCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			args := []string{}
			if action == "status" {
				args = append(args, "--no-pager")
			}
			return systemd.Control(append(args, action, systemd.ServiceName)...)
		},
	}
}

// isGoRunTempPath 判断二进制是否位于 go run 的临时构建目录。
func isGoRunTempPath(path string) bool {
	return strings.Contains(path, "go-build") || strings.HasPrefix(path, os.TempDir())
}

// askSecret 读入不显示回显的密钥。已有默认值时直接采用，不提示。
func askSecret(label, current string) (string, error) {
	if current != "" {
		return current, nil
	}
	fmt.Printf("请输入 %s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取 %s: %w", label, err)
	}
	value := strings.TrimSpace(string(b))
	if value == "" {
		return "", fmt.Errorf("%s 不能为空", label)
	}
	return value, nil
}

// askLine 读入一行普通文本，允许为空。
func askLine(label string) (string, error) {
	fmt.Printf("%s: ", label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取输入: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// writeConfigFile 以 0600 权限写入配置。文件含密钥，目录以 0700 创建。
func writeConfigFile(path string, cfg *config.Config) error {
	var b strings.Builder
	b.WriteString("# BreaCloud Telegram Bot 配置，由 `service install` 生成。\n")
	b.WriteString("# 内含密钥，权限应为 0600，不要提交到版本库。\n")
	b.WriteString("\ntelegram:\n")
	fmt.Fprintf(&b, "  bot_token: %q\n", cfg.Telegram.BotToken)
	if cfg.Telegram.OwnerChatID != 0 {
		fmt.Fprintf(&b, "  owner_chat_id: %d\n", cfg.Telegram.OwnerChatID)
	}
	b.WriteString("\nbreacloud:\n")
	fmt.Fprintf(&b, "  base_url: %q\n", cfg.BreaCloud.BaseURL)
	fmt.Fprintf(&b, "  api_token: %q\n", cfg.BreaCloud.APIToken)
	fmt.Fprintf(&b, "  concurrency: %d\n", cfg.BreaCloud.Concurrency)
	fmt.Fprintf(&b, "  timeout: %q\n", cfg.BreaCloud.Timeout.String())
	b.WriteString("\ndatabase:\n")
	fmt.Fprintf(&b, "  path: %q\n", cfg.Database.Path)
	b.WriteString("\nlog:\n")
	fmt.Fprintf(&b, "  level: %q\n", cfg.Log.Level)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("写入配置: %w", err)
	}
	return nil
}
