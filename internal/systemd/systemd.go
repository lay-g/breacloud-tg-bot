package systemd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// ServiceName 是 systemd 用户单元名。
const ServiceName = "breacloud-tg-bot.service"

// UnitParams 是渲染单元文件所需的参数。
type UnitParams struct {
	// ExecPath 是 serve 子命令所在的二进制绝对路径。
	ExecPath string
	// ConfigPath 是配置文件绝对路径。
	ConfigPath string
}

// runCommand 执行外部命令，测试中可替换。
var runCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// lookPath 查找外部命令，测试中可替换。
var lookPath = exec.LookPath

// Available 报告当前环境是否可以控制 systemd 用户实例。
func Available() bool {
	_, err := lookPath("systemctl")
	return err == nil
}

// RenderUnit 渲染单元文件内容。
func RenderUnit(p UnitParams) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=BreaCloud Telegram Bot\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s serve --config %s\n", quote(p.ExecPath), quote(p.ConfigPath))
	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n")
	b.WriteString("StandardOutput=journal\n")
	b.WriteString("StandardError=journal\n")
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

// quote 按 systemd 的转义规则包裹含空格或引号的路径。
func quote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Install 写入单元文件并启用服务。force=false 时拒绝覆盖已存在的单元文件。
func Install(unitPath string, p UnitParams, force bool) error {
	if !Available() {
		return errors.New("未找到 systemctl，无法安装 systemd 用户服务")
	}
	if _, err := os.Stat(unitPath); err == nil && !force {
		return fmt.Errorf("单元文件已存在: %s（如需覆盖请加 --force）", unitPath)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("检查 %s: %w", unitPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("创建 %s: %w", filepath.Dir(unitPath), err)
	}
	if err := os.WriteFile(unitPath, []byte(RenderUnit(p)), 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", unitPath, err)
	}
	if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	if err := runCommand("systemctl", "--user", "enable", ServiceName); err != nil {
		return fmt.Errorf("systemctl --user enable %s: %w", ServiceName, err)
	}
	// 用 restart 而不是 start：服务未运行时它会启动，已在运行时会把刚写入的配置生效。
	if err := runCommand("systemctl", "--user", "restart", ServiceName); err != nil {
		return fmt.Errorf("systemctl --user restart %s: %w", ServiceName, err)
	}
	return nil
}

// Uninstall 停用服务并删除单元文件。extraPaths 是 --purge 时需要一并删除的文件。
func Uninstall(unitPath string, purge bool, extraPaths ...string) error {
	if !Available() {
		return errors.New("未找到 systemctl，无法卸载 systemd 用户服务")
	}
	// 服务可能本来就没在运行，这里的失败不影响卸载本身。
	if err := runCommand("systemctl", "--user", "disable", "--now", ServiceName); err != nil {
		fmt.Fprintf(os.Stderr, "提示: systemctl --user disable --now %s 返回错误（可能本未启用）: %v\n", ServiceName, err)
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s: %w", unitPath, err)
	}
	if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %w", err)
	}
	if !purge {
		return nil
	}
	for _, p := range extraPaths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s: %w", p, err)
		}
	}
	return nil
}

// Control 执行 systemctl --user <args...>，输出直接透传到终端。
func Control(args ...string) error {
	if !Available() {
		return errors.New("未找到 systemctl，无法控制 systemd 用户服务")
	}
	return runCommand("systemctl", append([]string{"--user"}, args...)...)
}

// LingerEnabled 报告当前用户是否已开启 linger（决定用户服务能否开机自启、退出登录后继续运行）。
// 查询失败时返回错误，调用方应降级为提示用户自行确认，而不是中断流程。
func LingerEnabled() (bool, error) {
	current, err := user.Current()
	if err != nil {
		return false, fmt.Errorf("获取当前用户: %w", err)
	}
	cmd := exec.Command("loginctl", "show-user", current.Username, "-p", "Linger")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("loginctl show-user: %w", err)
	}
	return strings.TrimSpace(string(out)) == "Linger=yes", nil
}
