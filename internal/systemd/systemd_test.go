package systemd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubCommands 替换外部命令执行，返回记录调用的切片指针。
func stubCommands(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	oldRun, oldLook := runCommand, lookPath
	runCommand = func(name string, args ...string) error {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	lookPath = func(string) (string, error) { return "/usr/bin/systemctl", nil }
	t.Cleanup(func() { runCommand, lookPath = oldRun, oldLook })
	return &calls
}

func TestRenderUnit(t *testing.T) {
	got := RenderUnit(UnitParams{ExecPath: "/usr/local/bin/bot", ConfigPath: "/home/u/.config/bot/config.yaml"})

	for _, want := range []string{
		"ExecStart=/usr/local/bin/bot serve --config /home/u/.config/bot/config.yaml",
		"Restart=always",
		"WantedBy=default.target",
		"After=network-online.target",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("单元文件缺少 %q\n%s", want, got)
		}
	}
}

func TestRenderUnitQuotesPathsWithSpaces(t *testing.T) {
	got := RenderUnit(UnitParams{ExecPath: "/opt/my bot/bin", ConfigPath: "/home/a b/config.yaml"})
	if !strings.Contains(got, `ExecStart="/opt/my bot/bin" serve --config "/home/a b/config.yaml"`) {
		t.Errorf("含空格的路径未被引号包裹:\n%s", got)
	}
}

func TestInstallWritesUnitAndEnables(t *testing.T) {
	calls := stubCommands(t)
	unitPath := filepath.Join(t.TempDir(), "systemd", "user", ServiceName)

	err := Install(unitPath, UnitParams{ExecPath: "/bin/bot", ConfigPath: "/etc/config.yaml"}, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("读取单元文件: %v", err)
	}
	if !strings.Contains(string(content), "ExecStart=/bin/bot serve --config /etc/config.yaml") {
		t.Errorf("单元内容异常:\n%s", content)
	}
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("单元文件权限 = %o, want 644", perm)
	}

	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + ServiceName,
		"systemctl --user restart " + ServiceName,
	}
	if fmt.Sprint(*calls) != fmt.Sprint(want) {
		t.Errorf("调用序列 = %v, want %v", *calls, want)
	}
}

func TestInstallRefusesExistingUnitWithoutForce(t *testing.T) {
	stubCommands(t)
	unitPath := filepath.Join(t.TempDir(), ServiceName)
	if err := os.WriteFile(unitPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Install(unitPath, UnitParams{ExecPath: "/bin/bot"}, false); err == nil {
		t.Fatal("已存在的单元文件在未指定 --force 时应被拒绝")
	}
	if err := Install(unitPath, UnitParams{ExecPath: "/bin/bot"}, true); err != nil {
		t.Fatalf("--force 时应覆盖: %v", err)
	}
}

func TestUninstallRemovesUnitAndPurgesFiles(t *testing.T) {
	stubCommands(t)
	dir := t.TempDir()
	unitPath := filepath.Join(dir, ServiceName)
	if err := os.WriteFile(unitPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	purgePath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(purgePath, []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(unitPath, true, purgePath); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	for _, p := range []string{unitPath, purgePath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s 应被删除，实际 err=%v", p, err)
		}
	}
}

func TestUninstallKeepsFilesWithoutPurge(t *testing.T) {
	stubCommands(t)
	dir := t.TempDir()
	unitPath := filepath.Join(dir, ServiceName)
	keepPath := filepath.Join(dir, "config.yaml")
	for _, p := range []string{unitPath, keepPath} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := Uninstall(unitPath, false, keepPath); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("未加 --purge 时不应删除配置文件: %v", err)
	}
}
