// Package version 保存构建期注入的版本信息。
//
// 通过 -ldflags "-X github.com/lay-g/breacloud-tg-bot/internal/version.Version=..." 注入。
package version

var (
	// Version 是语义化版本号（形如 v0.1.0，来自仓库根目录的 VERSION 文件），未注入时为 dev。
	Version = "dev"
	// Commit 是构建所用的 git 提交，未注入时为空。
	Commit = ""
)
