package config

import (
	"os"
	"path/filepath"
)

// AppName 是程序名，同时作为配置目录、数据目录与 systemd 单元名的基名。
const AppName = "breacloud-tg-bot"

// ConfigDir 返回配置目录：$XDG_CONFIG_HOME/breacloud-tg-bot，未设置时回退 ~/.config。
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// DataDir 返回数据目录：$XDG_DATA_HOME/breacloud-tg-bot，未设置时回退 ~/.local/share。
func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, AppName), nil
}

// DefaultPath 返回默认配置文件路径。
func DefaultPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// DefaultDBPath 返回默认数据库文件路径。
func DefaultDBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bot.db"), nil
}

// UnitPath 返回 systemd 用户单元文件路径。
//
// 固定使用 ~/.config/systemd/user，不跟随 XDG_CONFIG_HOME：systemd 用户实例只认这个位置。
func UnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", AppName+".service"), nil
}
