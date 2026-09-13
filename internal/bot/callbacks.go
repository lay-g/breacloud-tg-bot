// Package bot 是 Telegram 交互层：启动、菜单、访问控制、视图与回调。
//
// 业务口径的计算不在这里（见 internal/jobs），本包只负责触发与展示。
// 面向 Telegram 的消息一律是纯文本，不设 parse_mode。
package bot

import (
	"strconv"
	"strings"
)

// 回调数据统一为竖线分隔的短字符串，单条不超过 64 字节。
//
// 回调类型：
//
//	m|main | vps | report | settings | help   主菜单与页面跳转
//	rg|<page>|<index>               区域列表翻页与下钻
//	rp|<page>|<index>               区域内 VPS 列表翻页
//	v|<serviceID>                   查看 VPS 详情
//	vf|<serviceID>                  强制刷新某台 VPS 的用量
//	a|<serviceID>|<action>          发起电源操作（进入二次确认）
//	ac|<serviceID>|<action>         确认执行电源操作
//	t|<serviceID>                   查看该 VPS 的最近任务
//	s|<field>|<value>               修改某个设置项
const (
	cbMain     = "m"
	cbRegion   = "rg"
	cbPage     = "rp"
	cbView     = "v"
	cbRefresh  = "vf"
	cbAction   = "a"
	cbConfirm  = "ac"
	cbTasks    = "t"
	cbSettings = "s"
)

// 主菜单的页面标识。
const (
	pageMain     = "main"
	pageVPS      = "vps"
	pageReport   = "report"
	pageSettings = "settings"
	pageHelp     = "help"
)

// Callback 是解析后的回调数据。
type Callback struct {
	Kind string
	Args []string
}

// ParseCallback 解析回调数据。格式不合法时返回 false，调用方直接忽略。
func ParseCallback(data string) (Callback, bool) {
	parts := strings.Split(data, "|")
	if len(parts) < 2 || parts[0] == "" {
		return Callback{}, false
	}
	return Callback{Kind: parts[0], Args: parts[1:]}, true
}

// ArgInt 返回第 i 个参数并解析为 int64。
func (c Callback) ArgInt(i int) (int64, bool) {
	if i < 0 || i >= len(c.Args) {
		return 0, false
	}
	v, err := strconv.ParseInt(c.Args[i], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// Arg 返回第 i 个参数，缺失时返回空串。
func (c Callback) Arg(i int) string {
	if i < 0 || i >= len(c.Args) {
		return ""
	}
	return c.Args[i]
}

// EncodeCallback 拼接回调数据。
func EncodeCallback(kind string, args ...string) string {
	parts := append([]string{kind}, args...)
	return strings.Join(parts, "|")
}

// 以下是最小可读的构造器，避免调用点到处拼字符串字面量。

func cbMenu(page string) string { return EncodeCallback(cbMain, page) }
func cbMainMenu() string        { return cbMenu(pageMain) }
func cbRegionAt(page, index int) string {
	return EncodeCallback(cbRegion, strconv.Itoa(page), strconv.Itoa(index))
}
func cbPageAt(page, index int) string {
	return EncodeCallback(cbPage, strconv.Itoa(page), strconv.Itoa(index))
}
func cbService(id int64) string { return EncodeCallback(cbView, strconv.FormatInt(id, 10)) }
func cbRefreshService(id int64) string {
	return EncodeCallback(cbRefresh, strconv.FormatInt(id, 10))
}
func cbPowerAction(id int64, action string) string {
	return EncodeCallback(cbAction, strconv.FormatInt(id, 10), action)
}
func cbPowerConfirm(id int64, action string) string {
	return EncodeCallback(cbConfirm, strconv.FormatInt(id, 10), action)
}
func cbServiceTasks(id int64) string {
	return EncodeCallback(cbTasks, strconv.FormatInt(id, 10))
}
func cbSetting(field, value string) string {
	return EncodeCallback(cbSettings, field, value)
}

// powerActionDef 描述一个允许下发的电源动作。
//
// 语义来自接口文档：shutdown 是温和关机，stop 是冷关机（等价于拔电）。
type powerActionDef struct {
	Action  string
	Label   string
	Warning string
}

var powerActions = []powerActionDef{
	{"start", "开机", "确认开机？"},
	{"reboot", "重启", "确认重启？会先尝试温和关机再启动。"},
	{"shutdown", "关机", "确认关机？会先向系统发送关机信号。"},
	{"cold_reboot", "冷重启", "确认冷重启？相当于直接断电再上电。"},
	{"stop", "冷关机", "确认冷关机？会立即断电，未落盘的数据会丢失。"},
}

// powerAction 按动作名查找定义。
func powerAction(action string) (powerActionDef, bool) {
	for _, a := range powerActions {
		if a.Action == action {
			return a, true
		}
	}
	return powerActionDef{}, false
}
