# bot — Telegram 交互

## 职责

接收 Telegram 更新、校验访问权限、把用户操作翻译成对 `breacloud` 与 `store` 的调用、渲染纯文本回复与按钮。**不直接计算业务口径**：日报、预警这类聚合逻辑在 `jobs` 里，`bot` 只负责触发与展示。

## 文件划分

- `bot.go`：启动、选项装配、更新分发、访问控制、`Send` / `Broadcast`（实现 `notify.Notifier`）。
- `settings_panel.go`：设置项的纯函数修改逻辑（`applySetting`）。
- `vps.go`：区域列表、VPS 详情、电源操作与任务查看。
- `menu.go`：`SetMyCommands`，让客户端输入框出现命令列表。
- `handlers.go`：命令处理。
- `callbacks.go`：按钮回调处理。
- `views.go`：交互界面的纯文本渲染（菜单、VPS 列表、详情、设置、任务），输入数据、输出字符串，可单测。日报与预警的文案不在这里，它们在 `internal/jobs/views.go`——那是任务自身的产物，不是交互界面的一部分。
- `keyboards.go`：`models.InlineKeyboardMarkup` 构造。

## 启动顺序

1. `bot.New(token, opts...)`，用 `WithMessageTextHandler` / `WithCallbackQueryDataHandler` 注册各前缀。
2. `SetMyCommands` 注册菜单。
3. `b.Start(ctx)` 阻塞直到 ctx 结束（长轮询）。

菜单在每次启动时重新注册，因此「菜单自动初始化」不需要任何人工步骤，改了命令重启即可生效。

## 访问控制

放在更新分发入口，所有 handler 第一行调用同一个 `authorize(ctx, chatID)`：

- 在白名单内：通过。
- 不在白名单内且消息是 `/start` 或 `/menu`：回一条「未授权」并附带该 chat 自己的 id，方便本人复制给管理员。
- 不在白名单内且是其它消息：**静默忽略**，不回复。否则任何人都能用这个 Bot 探测它是否存在。

`/allow`、`/deny`、`/allowlist` 注册为处理器但不进菜单，只在 `/help` 文本里列出。不做角色分级——白名单本身就是唯一的权限边界，等真需要第二级再加 `chats.role`。

## 首次招呼

`authorize` 通过后检查 `store.NeedGreeting`，为真则发功能介绍并 `MarkGreeted`。判定状态放在数据库而不是内存，避免重启后被重复欢迎。

## 消息与按钮

**全部使用 MarkdownV2。** 代价是转义必须做全：MarkdownV2 要求 `_ * [ ] ( ) ~ \` > # + - = | { } . !` 在正文里转义，漏一个是**整条消息被拒收**而不是排版错乱。因此：

- 所有动态文本过 `md.Escape`；IP、日期这类适合等宽的用 `md.Code`（code 实体内部只需转义反引号与反斜杠）。
- 任何实体都在**一行内**闭合，因为超长消息按行截断——否则截断点会切出一个未闭合的实体。
- 发送与编辑都带降级兜底：识别到 `can't parse entities` 就退回纯文本重发一次，同时打 error 日志。兜底是安全网，不是常规路径。
- 这套约定由真实 Bot API 的集成测试守着（见 `markdown_integration_test.go`）。

回调数据统一为竖线分隔的短字符串，单条不超过 64 字节。区域名可能超长，所以区域列表用「按名称排序后的下标」引用，回调处理时重新取一次列表并按同一规则排序，下标越界回「列表已更新，请重新打开」。

渲染函数统一保证输出不超过 3900 字符（Telegram 上限 4096），超出时按行截断并追加一行「……另有 N 项未显示」。

## 为什么不用 ForceReply 做数值输入

设置面板最初考虑「点按钮 → ForceReply 输入文本」。放弃的原因不是难度，而是状态机：需要记住「哪条消息在等哪个字段的输入」，还要处理用户不回复、回复别的内容、并发多个提示等分支，全部状态都会变成 Bot 层的隐藏状态。

改成纯按钮后没有状态：报告时间用「早一点 / 晚一点」按 30 分钟步进，阈值用 70/80/90/100 多选，提前天数用 1/2/3/5/7 单选。改完就地 `EditMessageText` 重渲染同一条消息，用户能立刻看到结果。

## 危险操作的确认

`a|<serviceID>|<action>` 只渲染确认消息，`ac|<serviceID>|<action>` 才真正调用 API。确认文案说明后果（例如冷关机是「立即断电」），取消按钮回到详情页。

执行成功后只回「已下发」并附「查看任务」按钮。**不自动轮询任务**：大账号下轮询会显著增加请求量，而该接口已经实测出偶发超时。用户想看进度就点按钮，按需查一次。

## 视图

- 主菜单：区域列表入口 + 报告 + 设置。
- 区域列表：每个区域一行「区域名 · N 台」，超过 8 个分页。
- 区域内 VPS：每页 10 台，一行「状态 + 名称 + 主 IP」。
- VPS 详情：`GET /services/:id` 与 `GET /services/:id/traffic` 并发取，叠加 `store` 里近 7 天的日用量；按钮为开机、关机、重启、冷重启、冷关机、刷新用量、查看任务、返回。

区域粒度是城市级（`region_name`），因为线路级必须逐台调详情接口再经 `/meta/locations` 映射，而该映射因主键重复不保证唯一。

## 可测试性

`views.go` 与 `callbacks.go` 的解析逻辑都是纯函数，可以完全不碰 Telegram 测试。整条链路另有一条 dry-run 通路：`internal/notify.Notifier` 的另一个实现把消息打到 stdout，`serve --dry-run` 因此能在没有 Bot token 的环境中跑通取数与渲染。
