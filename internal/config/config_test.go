package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置: %v", err)
	}
	return path
}

func TestLoadReadsFileValues(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	path := writeConfig(t, `
telegram:
  bot_token: "123:abc"
  owner_chat_id: 42

breacloud:
  base_url: "https://example.test/api/v1/"
  api_token: "bll_test"
  concurrency: 3
  timeout: "5s"

database:
  path: "/tmp/x.db"

log:
  level: "DEBUG"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telegram.BotToken != "123:abc" || cfg.Telegram.OwnerChatID != 42 {
		t.Errorf("telegram = %+v", cfg.Telegram)
	}
	// base_url 末尾斜杠必须被去掉，否则拼接出 //services
	if cfg.BreaCloud.BaseURL != "https://example.test/api/v1" {
		t.Errorf("base_url = %q", cfg.BreaCloud.BaseURL)
	}
	if cfg.BreaCloud.Concurrency != 3 || cfg.BreaCloud.Timeout != 5*time.Second {
		t.Errorf("breacloud = %+v", cfg.BreaCloud)
	}
	if cfg.Database.Path != "/tmp/x.db" {
		t.Errorf("database = %+v", cfg.Database)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %q", cfg.Log.Level)
	}
	if cfg.Path != path {
		t.Errorf("path = %q", cfg.Path)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BreaCloud.BaseURL != "https://brea.cloud/api/v1" {
		t.Errorf("base_url = %q", cfg.BreaCloud.BaseURL)
	}
	if cfg.BreaCloud.Concurrency != 5 {
		t.Errorf("concurrency = %d", cfg.BreaCloud.Concurrency)
	}
	if cfg.BreaCloud.Timeout != 20*time.Second {
		t.Errorf("timeout = %v", cfg.BreaCloud.Timeout)
	}
	if want := filepath.Join(dataDir, AppName, "bot.db"); cfg.Database.Path != want {
		t.Errorf("database path = %q, want %q", cfg.Database.Path, want)
	}
}

// 配置文件不存在不算读取错误：容器部署常常只用环境变量。
// 真正该失败的是「值没凑齐」，而且报错要能说清缺的是哪个。
func TestLoadToleratesMissingFileButValidateFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	cfg, err := Load(missing)
	if err != nil {
		t.Fatalf("文件缺失时 Load 不该报错: %v", err)
	}
	err = cfg.Validate()
	if err == nil {
		t.Fatal("没有任何来源提供 token 时 Validate 应报错")
	}
	if !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("报错未说明文件不存在: %v", err)
	}

	// 环境变量补齐后应当通过校验，这正是容器部署的用法
	t.Setenv(EnvPrefix+"_TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv(EnvPrefix+"_BREACLOUD_API_TOKEN", "bll_env")
	t.Setenv(EnvPrefix+"_DATABASE_PATH", "/data/bot.db")

	cfg, err = Load(missing)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("纯环境变量配置应通过校验: %v", err)
	}
	if cfg.Database.Path != "/data/bot.db" {
		t.Errorf("database.path = %q", cfg.Database.Path)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(EnvPrefix+"_TELEGRAM_BOT_TOKEN", "999:env")

	path := writeConfig(t, "telegram:\n  bot_token: \"123:file\"\nbreacloud:\n  api_token: \"bll_file\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telegram.BotToken != "999:env" {
		t.Errorf("环境变量未覆盖配置文件: %q", cfg.Telegram.BotToken)
	}
	if cfg.BreaCloud.APIToken != "bll_file" {
		t.Errorf("未被覆盖的字段应保留文件值: %q", cfg.BreaCloud.APIToken)
	}
}

func TestValidate(t *testing.T) {
	ok := &Config{
		Telegram:  TelegramConfig{BotToken: "123:abc"},
		BreaCloud: BreaCloudConfig{APIToken: "bll_x"},
		Database:  DatabaseConfig{Path: "/tmp/x.db"},
		Path:      "/tmp/config.yaml",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法配置被拒绝: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"缺 bot token", func(c *Config) { c.Telegram.BotToken = "" }, "telegram.bot_token"},
		{"缺 api token", func(c *Config) { c.BreaCloud.APIToken = "" }, "breacloud.api_token"},
		{"缺数据库路径", func(c *Config) { c.Database.Path = "" }, "database.path"},
		{"api token 前缀错", func(c *Config) { c.BreaCloud.APIToken = "abc" }, "bll_"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone := *ok
			tc.mutate(&clone)
			err := clone.Validate()
			if err == nil {
				t.Fatal("期望报错，实际通过")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误信息 %q 未包含 %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestPathHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	if got, _ := ConfigDir(); got != filepath.Join(home, ".config", AppName) {
		t.Errorf("ConfigDir = %q", got)
	}
	if got, _ := DataDir(); got != filepath.Join(home, ".local", "share", AppName) {
		t.Errorf("DataDir = %q", got)
	}
	// systemd 用户单元位置固定，不跟随 XDG_CONFIG_HOME
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, _ := UnitPath(); got != filepath.Join(home, ".config", "systemd", "user", AppName+".service") {
		t.Errorf("UnitPath = %q", got)
	}
}
