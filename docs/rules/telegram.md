# MarkdownV2 与 Telegram 消息格式

## 未转义的保留字符会让整条消息发不出去

**现象**：把接口返回的服务名、区域名或错误消息直接拼进文本，`sendMessage` 返回
`400 Bad Request: can't parse entities: Character '-' is reserved and must be escaped with the preceding '\'`。
不是渲染错位，是**整条消息被拒收**，用户什么也收不到。

**原因**：MarkdownV2 要求 `_ * [ ] ( ) ~ \` > # + - = | { } . !` 这 18 个字符在正文里
必须转义。服务名里的 `-`、日期里的 `.` 和 `-`、`+20%` 里的 `+` 都在射程内。

**解决**：

- 所有动态文本进模板前过 `internal/md.Escape`；IP、日期这类适合等宽的用 `md.Code`（code 内部只需转义反引号与反斜杠）。
- 静态模板文字里手写转义，中文标点优先用全角（`：。！`）以避开保留字符。
- 发送侧保留兜底：识别到 `can't parse entities` 就降级成纯文本重发一次，宁可难看也不能不发。

**相关文件**：`internal/md/md.go`、`internal/bot/bot.go`

## markdownv2 的实体不能跨行

**现象**：消息超长时按行截断，截断点正好落在 `*粗体` 中间，整条消息被拒收。

**原因**：按字节或按行截断都可能切开一个未闭合的实体。

**解决**：约定「任何实体都在一行内闭合」，所有渲染函数都遵守；截断只按整行丢弃。
这条约定是 `Truncate` 能安全工作的前提，改动渲染函数时必须一起检查。

**相关文件**：`internal/bot/views.go`、`internal/jobs/views.go`

## 拿真实 Bot API 当 MarkdownV2 校验器

**现象**：本地单测只能断言「文本里有没有转义符」，无法证明 Telegram 会接受它。

**原因**：MarkdownV2 没有公开的解析库，规则细节（实体嵌套、块引用、代码块）容易记错。

**解决**：Telegram **先解析实体、再校验会话**。把消息发到不存在的 `chat_id=1`：

- 语法合法 → `400 chat not found`
- 语法非法 → `400 can't parse entities: ...`

于是可以写一个只为校验而存在的集成测试，覆盖所有渲染函数，且不会打扰任何人。
测试里还要反过来验证校验器本身可信——故意发一条未闭合的实体，确认它会被判为非法。

**相关文件**：`markdown_integration_test.go`

## 库里的 ParseMode 常量名有歧义

**现象**：想用 MarkdownV2，看到 `models.ParseModeMarkdown` 和 `models.ParseModeMarkdownV1` 两个常量，很容易选错。

**原因**：go-telegram/bot 里 `ParseModeMarkdown` 的值是 `"MarkdownV2"`，`ParseModeMarkdownV1` 才是旧的 `"Markdown"`。

**解决**：在 `internal/bot` 里定义 `const parseMode = models.ParseModeMarkdown` 并写上注释，避免调用点误用。

**相关文件**：`internal/bot/bot.go`
