// Package md 负责 MarkdownV2 的转义与实体构造。
//
// Telegram 的 MarkdownV2 规则很硬：`_ * [ ] ( ) ~ ` > # + - = | { } . !` 这 18 个
// 字符在正文里必须用反斜杠转义，否则整条消息会被拒收（不是渲染错，是发送失败）。
// 因此凡是来自接口或用户的数据，进了模板之前都必须先过 Escape。
//
// 官方规则：https://core.telegram.org/bots/api#markdownv2-style
package md

import "strings"

// reserved 是正文里必须转义的字符集合。
const reserved = `_*[]()~` + "`" + `>#+-=|{}.!`

// Escape 转义正文里的保留字符与反斜杠本身。
//
// 多余地转义非保留字符是无害的（官方明确说明 1~126 的字符都可以被转义），
// 所以这里的策略是「宁可多转义，不可漏」。
func Escape(s string) string {
	if !strings.ContainsAny(s, reserved+`\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	for _, r := range s {
		if r == '\\' || strings.ContainsRune(reserved, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Unescape 去掉反斜杠转义，用于降级成纯文本发送。
func Unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// EscapeCode 按 code 与 pre 实体内部的规则转义：只需要处理反引号与反斜杠。
func EscapeCode(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if r == '`' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EscapeURL 按行内链接 (...) 部分的规则转义：只需要处理右括号与反斜杠。
func EscapeURL(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if r == ')' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Bold 把原始文本渲染成粗体。
func Bold(raw string) string { return "*" + Escape(raw) + "*" }

// Italic 把原始文本渲染成斜体。
func Italic(raw string) string { return "_" + Escape(raw) + "_" }

// Underline 把原始文本渲染成下划线。
func Underline(raw string) string { return "__" + Escape(raw) + "__" }

// Strike 把原始文本渲染成删除线。
func Strike(raw string) string { return "~" + Escape(raw) + "~" }

// Spoiler 把原始文本渲染成剧透。
func Spoiler(raw string) string { return "||" + Escape(raw) + "||" }

// Code 把原始文本渲染成行内等宽代码。
func Code(raw string) string { return "`" + EscapeCode(raw) + "`" }

// Pre 把原始文本渲染成代码块。语言名可留空。
func Pre(language, raw string) string {
	return "```" + language + "\n" + EscapeCode(raw) + "\n```"
}

// Link 生成行内链接。显示文本按正文规则转义，URL 按链接规则转义。
func Link(text, url string) string {
	return "[" + Escape(text) + "](" + EscapeURL(url) + ")"
}

// Quote 生成块引用，每行带上 "> " 前缀，因此整段可以按行安全截断。
//
// 与 Bold 等一致，入参是原始文本，内部负责转义；因此不能在块引用里嵌实体。
func Quote(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, "> "+Escape(line))
	}
	return strings.Join(out, "\n")
}

// Label 生成「加粗标签 + 值」的一行，值按正文规则转义。
func Label(label, value string) string {
	return "*" + label + "* " + Escape(value)
}
