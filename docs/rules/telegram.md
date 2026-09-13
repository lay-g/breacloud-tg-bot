# Telegram / go-telegram/bot

## `MatchTypeCommand` 匹配不了 `/cmd@botname`

**现象**：用 `bot.WithMessageTextHandler("/start", bot.MatchTypeCommand, h)` 注册，用户在群聊里发送 `/start@某个机器人` 时处理器不触发。

**原因**：库的实现是把消息实体里的命令文本整段与 pattern 比较，`start@botname` 不等于 `start`。

**解决**：不用命令匹配，改用 `WithDefaultHandler` 收下所有更新，自己按前缀解析命令：切掉开头的 `/`，再切掉 `@` 之后的机器人名。这样 `/start`、`/START`、`/start@bot` 都能落到同一个分支。

**相关文件**：`internal/bot/bot.go`（`parseCommand`）

## 回调数据有 64 字节上限

**现象**：把区域名直接写进回调数据，区域名是中文且较长时可能超限，Telegram 会拒绝整个键盘。

**原因**：`callback_data` 硬上限 64 字节。

**解决**：回调数据一律用竖线分隔的短标识；区域这类可能超长的对象改用「对区域名排序后的下标」引用，回调时重新取列表并按同一规则排序，下标越界则回一句「列表已更新，请重新打开」。

**相关文件**：`internal/bot/callbacks.go`

## 回调消息可能已经不可访问

**现象**：机器人重启或消息过久后点击旧消息上的按钮，取消息内容时拿到的是 `InaccessibleMessage`。

**原因**：Telegram 把过旧的 `message` 替换成只有 id/date 的占位对象。

**解决**：`CallbackQuery.Message` 是 `MaybeInaccessibleMessage`，先判断 `Type` 再取 `Message`；不可访问时只调用一次 `AnswerCallbackQuery` 把客户端的转圈停掉。

**相关文件**：`internal/bot/bot.go`（`onCallback`）

## `editMessageText` 内容完全相同时会报错

**现象**：反复点击刷新按钮后，`editMessageText` 返回 `message is not modified` 错误。

**原因**：Telegram 拒绝把消息改成与当前完全一致的内容。

**解决**：`edit` 封装里捕获失败后改为发送一条新消息，用户永远能拿到反馈，而不是点下去没反应。

**相关文件**：`internal/bot/bot.go`（`edit`）

## 消息长度上限 4096，必须主动截断

**现象**：大账号下区域/任务列表拼出来超过 4096 字符，整条消息发送失败。

**原因**：Telegram 的文本消息上限。

**解决**：所有对外发送的文本都过一次 `Truncate`，超过 3900 字符时按行截断并追加「……另有 N 行未显示」，留出余量。

**相关文件**：`internal/bot/views.go`（`Truncate`）
