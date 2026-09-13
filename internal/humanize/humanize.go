// Package humanize 提供展示层的格式化：字节大小、时间、开关状态。
//
// 独立成包是为了让 bot（交互界面）与 jobs（报告与预警文案）共用同一套口径，
// 避免同一个数字在两处显示成不同的样子。
package humanize

import (
	"fmt"
	"time"
)

// Bytes 输出 1024 进制的人类可读大小。
//
// 单位标为 GB 而不是 GiB：BreaCloud 面板与接口的 used_gb / quota_gb 都是 1024
// 进制却写作 GB，与面板保持一致比单位学究更重要。
func Bytes(n int64) string {
	if n < 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	i := -1
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

// Memory 把 MB 输出成内存大小，超过 1 GB 时改用 GB。
func Memory(mb int64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.0f GB", float64(mb)/1024)
	}
	return fmt.Sprintf("%d MB", mb)
}

// Date 把 RFC3339 压成 YYYY-MM-DD，解析失败时原样截取前 10 个字符。
func Date(s string) string {
	if s == "" {
		return "-"
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("2006-01-02")
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// DateTime 把 RFC3339 压成 MM-DD HH:MM。
func DateTime(s string) string {
	if s == "" {
		return "-"
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("01-02 15:04")
	}
	if len(s) >= 16 {
		return s[5:16]
	}
	return s
}

// OnOff 把布尔值渲染成中文开关状态。
func OnOff(v bool) string {
	if v {
		return "已开启"
	}
	return "已关闭"
}
