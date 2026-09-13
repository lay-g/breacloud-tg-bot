# jobs — 定时任务

## 职责

按时间触发三类推送：每日流量报告、周期机到期提醒、流量阈值预警。取数、聚合与**推送文案**都在这里，怎么发出去交给 `notify.Notifier`。

文案放在这一层而不是 `bot`：日报内容与预警内容是任务的产物，跟交互界面无关；`bot` 只在用户点「昨日报告」时通过注入的 `ReportFunc` 调一次 `BuildDailyReport`，两边共用同一个渲染函数。

## 依赖注入

    type Deps struct {
        Store  *store.Store
        API    *breacloud.Client
        Cache  *breacloud.ServiceCache
        Notify notify.Notifier
        Log    *slog.Logger
        Now    func() time.Time
    }

`Now` 是可替换的时钟，让「昨天是哪天」「还剩几天到期」这类判断能在测试里固定。`Notify` 是接口，因此任务逻辑可以在没有 Telegram 的环境里完整跑一遍。

    type Notifier interface {
        Send(ctx context.Context, chatID int64, text string) error
        Broadcast(ctx context.Context, text string) error
    }

两个实现：`bot.Bot`（真实发送）与 `cli` 里的 dry-run（打到 stdout）。

## 调度

两个 ticker 同在一个 `Run` 里：

- **每日任务**：每 30 秒检查一次。判定条件是「主机本地时间已过 `settings.report_time`」且 `daily_job_runs` 里没有今天这一行。用「已过某个时刻」而不是「等于某个时刻」，是为了让停机错过的任务在当天启动后补跑一次。
- **流量预警**：每 60 分钟一次，启动后 30 秒先跑一次。重启后立刻跑不会造成重复推送，因为去重状态在数据库里。

## 每日任务

一次执行内做三件事，共用一条 `daily_job_runs` 标记：刷新所有 VPS 的日用量缓存、推送日报（若开关打开）、推送到期提醒（若开关打开）。

顺序是先取数落库、再渲染、再发送，**发送成功后才写标记**。

取数部分失败不阻断：能取到的机器照常统计，失败台数写进消息末尾（例如「3 台数据获取失败」）。宁可发一份不完整但明确的报告，也不要因为一台机器超时就不发。

如果当天整个任务失败，标记不会写入，30 秒后会重试。如果只是发送失败，标记同样不会写入——但 Telegram 发送失败通常是 token 或网络问题，持续重试会每 30 秒失败一次；因此发送失败会记 error 日志并**跳过当天的重试**（写入标记），手动补发走 Bot 内的 `/report` 命令，它复用同一个渲染函数。

## 日报口径

- 目标日 = `time.Now().In(UTC+8).AddDate(0,0,-1)`，格式化为 `YYYY-MM-DD` 后去匹配日桶的 `bucket` 原值。
- 每台服务取 `traffic-history?range=week`（不是 `day`，原因见 `docs/references/breacloud-api.md`），在返回的 8 个日桶里找目标日；找不到按 0 计并计入失败台数。
- 成功时把返回的全部日桶 upsert 进 `daily_usage`，一次请求顺便把缓存补满 8 天。
- 总量 = 所有服务目标日的 `in_bytes + out_bytes` 之和；前五按总量降序，同值时按名称升序，保证输出稳定。
- 消息同时给出与前一日的环比。

并发用固定 5 个 worker，退避重试在客户端里。大几百台时一轮约几十秒，可接受。

## 到期提醒

只处理周期机：`billing_mode == "periodic"`（或 `next_due_date` 非空且 `expire_at` 为空）。小时机的 `expire_at` 不参与。

`daysLeft = ceil((due - now) / 24h)`，保留 `0 <= daysLeft <= settings.expiry_days` 的项，按剩余天数升序。文案按三种状态区分：`renew_canceled_at` 非空是「到期释放且不再续费」，`auto_renew_enabled` 为真是「将自动续费」，其余是「需手动续费」。

「每天一条合并消息」既是提醒强度也是幂等：重复执行只会在同一天内重复发送，由 `daily_job_runs` 挡住。列表为空时不发消息。

## 流量预警的状态机

每个 `(service_id, threshold)` 有一个「是否已解除武装」的状态，存在 `traffic_alert_state.alerted_at`：

1. `percent >= threshold` 且 `alerted_at` 为空 → 本次推送，并写入 `alerted_at`。
2. `percent < threshold` 且 `alerted_at` 非空 → 清空 `alerted_at`（重新武装），本次不推送。
3. 其余情况不动。

不用计费周期做去重键是刻意的：配额重置、购买流量包、周期切换都会让比值掉回阈值以下，状态机会自动重新武装，因此不需要额外监听 `period_start` 是否变化。

`percent` 用整数运算：`(in_bytes + out_bytes) * 100 / (quota_gb << 30)`。`quota_gb` 是 GiB 不是 GB，`unlimited=true` 的机器直接跳过。

一次检查中所有待推送项合并成一条消息广播；超过 30 条时分条发送，避免撞上 Telegram 的消息长度上限。

## 为什么不做「每台一条」

大账号下每台一条会把一次检查变成几百条消息，既触发 Telegram 的频率限制，也让用户无法一眼看出全局。合并成一条 + 按用量排序，信息密度更高。

## 相关文档

- 预警状态表：`docs/design/store.md`
- 日桶的时区与取数口径：`docs/references/breacloud-api.md`
