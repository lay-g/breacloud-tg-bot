// Package config 负责加载、校验并暴露运行配置。
//
// 配置来源优先级：环境变量 > 配置文件 > 内置默认值。
// 环境变量前缀为 BREACLOUD_TG_BOT_，层级用下划线连接，例如
// BREACLOUD_TG_BOT_TELEGRAM_BOT_TOKEN 对应 telegram.bot_token。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// EnvPrefix 是所有环境变量的前缀。
const EnvPrefix = "BREACLOUD_TG_BOT"

// TelegramConfig 保存 Telegram 侧凭据。OwnerChatID 为 0 表示未设置。
type TelegramConfig struct {
	BotToken    string
	OwnerChatID int64
}

// BreaCloudConfig 保存 BreaCloud API 访问参数。
type BreaCloudConfig struct {
	BaseURL     string
	APIToken    string
	Concurrency int
	Timeout     time.Duration
}

// DatabaseConfig 保存 SQLite 数据库位置。
type DatabaseConfig struct {
	Path string
}

// LogConfig 保存日志参数。
type LogConfig struct {
	Level string
}

// Config 是完整运行配置。Path 记录实际加载的配置文件路径，便于日志与排错。
type Config struct {
	Telegram  TelegramConfig
	BreaCloud BreaCloudConfig
	Database  DatabaseConfig
	Log       LogConfig
	Path      string
}

// Load 从 path 加载配置，path 为空时使用 DefaultPath。
// 配置文件不存在时返回错误，并提示先执行 service install。
func Load(path string) (*Config, error) {
	return load(path, true)
}

// LoadOrDefault 与 Load 相同，但配置文件不存在时返回默认配置（仅含默认值与环境变量）。
// 供 service install 在首次安装时复用已有设置。
func LoadOrDefault(path string) (*Config, error) {
	return load(path, false)
}

func load(path string, required bool) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, fmt.Errorf("确定默认配置路径: %w", err)
		}
		path = p
	}

	dbPath, err := DefaultDBPath()
	if err != nil {
		return nil, fmt.Errorf("确定默认数据库路径: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("breacloud.base_url", "https://brea.cloud/api/v1")
	v.SetDefault("breacloud.concurrency", 5)
	v.SetDefault("breacloud.timeout", "20s")
	v.SetDefault("database.path", dbPath)
	v.SetDefault("log.level", "info")
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	switch err := v.ReadInConfig(); {
	case err == nil:
	case os.IsNotExist(err) && !required:
	default:
		return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}

	cfg := &Config{
		Telegram: TelegramConfig{
			BotToken:    strings.TrimSpace(v.GetString("telegram.bot_token")),
			OwnerChatID: v.GetInt64("telegram.owner_chat_id"),
		},
		BreaCloud: BreaCloudConfig{
			BaseURL:     strings.TrimRight(v.GetString("breacloud.base_url"), "/"),
			APIToken:    strings.TrimSpace(v.GetString("breacloud.api_token")),
			Concurrency: v.GetInt("breacloud.concurrency"),
			Timeout:     v.GetDuration("breacloud.timeout"),
		},
		Database: DatabaseConfig{
			Path: v.GetString("database.path"),
		},
		Log: LogConfig{
			Level: strings.ToLower(v.GetString("log.level")),
		},
		Path: path,
	}
	if cfg.BreaCloud.Concurrency < 1 {
		cfg.BreaCloud.Concurrency = 1
	}
	if cfg.BreaCloud.Timeout <= 0 {
		cfg.BreaCloud.Timeout = 20 * time.Second
	}
	return cfg, nil
}

// Validate 检查启动所必需的值是否齐备。
func (c *Config) Validate() error {
	var missing []string
	if c.Telegram.BotToken == "" {
		missing = append(missing, "telegram.bot_token")
	}
	if c.BreaCloud.APIToken == "" {
		missing = append(missing, "breacloud.api_token")
	}
	if c.Database.Path == "" {
		missing = append(missing, "database.path")
	}
	if len(missing) > 0 {
		return fmt.Errorf("配置 %s 缺少必填项: %s（可运行 `%s service install` 补齐）",
			c.Path, strings.Join(missing, ", "), AppName)
	}
	if !strings.HasPrefix(c.BreaCloud.APIToken, "bll_") {
		return errors.New("breacloud.api_token 格式不正确：应以 bll_ 开头")
	}
	return nil
}
