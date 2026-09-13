package md

import (
	"strings"
	"testing"
)

// allReserved 是官方规则里要求转义的字符全集，顺带覆盖反斜杠。
const allReserved = `_*[]()~\` + "`" + `>#+-=|{}.!\`

func TestEscapeCoversEveryReservedCharacter(t *testing.T) {
	got := Escape(allReserved)
	for _, r := range allReserved {
		if !strings.Contains(got, `\`+string(r)) {
			t.Errorf("字符 %q 未被转义: %q", r, got)
		}
	}
	// 转义后必须能原样还原：这比数反斜杠可靠（反斜杠自身会被转义成两个）
	if Unescape(got) != allReserved {
		t.Errorf("还原失败: %q", got)
	}
}

func TestEscapeLeavesHarmlessTextAlone(t *testing.T) {
	// 中文与空格不需要转义，避免输出里塞满无意义的反斜杠
	const plain = "洛杉矶 Pro CN2GIA 中文"
	if got := Escape(plain); got != plain {
		t.Errorf("Escape(%q) = %q", plain, got)
	}
	// 数字与字母也不需要
	if got := Escape("vps gia 185197 ABC"); got != "vps gia 185197 ABC" {
		t.Errorf("Escape 改动了普通文本: %q", got)
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	cases := []string{
		allReserved,
		"vps-gia-185197（Los Angeles）",
		"28.82 GB / 2000 GB（21%）",
		"★ 特殊符号 ★",
		"",
	}
	for _, in := range cases {
		if got := Unescape(Escape(in)); got != in {
			t.Errorf("往返失败: %q -> %q -> %q", in, Escape(in), got)
		}
	}
}

func TestCodeAndPreEscapeOnlyBacktickAndBackslash(t *testing.T) {
	// 代码实体内部点号、减号是安全的，不该被转义
	if got := Code("28.82-GB"); got != "`28.82-GB`" {
		t.Errorf("Code = %q", got)
	}
	if got := Code("a`b\\c"); got != "`a\\`b\\\\c`" {
		t.Errorf("Code 未正确转义反引号: %q", got)
	}
	if got := Pre("bash", "echo `x` && echo \\"); !strings.HasPrefix(got, "```bash\n") || !strings.HasSuffix(got, "\n```") {
		t.Errorf("Pre = %q", got)
	}
}

func TestLinkEscapesBothSides(t *testing.T) {
	got := Link("点 这里(新)", "https://example.com/a)b")
	if !strings.HasPrefix(got, `[点 这里\(新\)]`) {
		t.Errorf("显示文本未转义: %q", got)
	}
	if !strings.HasSuffix(got, `(https://example.com/a\)b)`) {
		t.Errorf("URL 未转义右括号: %q", got)
	}
}

func TestQuotePrefixesEveryLine(t *testing.T) {
	got := Quote("第一行", "含.点号")
	want := "> 第一行\n> 含\\.点号"
	if got != want {
		t.Errorf("Quote = %q, want %q", got, want)
	}
}

func TestEntitiesWrapEscapedText(t *testing.T) {
	if got := Bold("a*b"); got != `*a\*b*` {
		t.Errorf("Bold = %q", got)
	}
	if got := Spoiler("x_y"); got != `||x\_y||` {
		t.Errorf("Spoiler = %q", got)
	}
	if got := Label("总量：", "28.82 GB"); got != `*总量：* 28\.82 GB` {
		t.Errorf("Label = %q", got)
	}
}
