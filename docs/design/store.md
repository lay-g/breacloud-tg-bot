# store — 持久层

> 状态：设计中（M2 实现）。

## 职责

SQLite 的 schema、查询与业务读写方法。上层只看到 Go 方法，不接触 sqlc 生成的类型，也不写裸 SQL。

数据库**只存可重建的状态与设置**，不存凭据。删掉 `bot.db` 的后果是：白名单退回到配置文件里的 `owner_chat_id`、设置回到默认值、日用量缓存重新抓取、预警可能在重新武装后多推一次——没有任何不可恢复的损失。

## 表结构

    CREATE TABLE IF NOT EXISTS chats (
      chat_id    INTEGER PRIMARY KEY,
      greeted_at TEXT,
      created_at TEXT NOT NULL
    );

白名单。`greeted_at` 为 NULL 表示还没发过首次招呼。写入这个表就等于授权该 chat 访问 Bot。

    CREATE TABLE IF NOT EXISTS settings (
      id                     INTEGER PRIMARY KEY CHECK (id = 1),
      report_enabled         INTEGER NOT NULL DEFAULT 1,
      report_time            TEXT    NOT NULL DEFAULT '09:00',
      traffic_alert_enabled  INTEGER NOT NULL DEFAULT 1,
      traffic_thresholds     TEXT    NOT NULL DEFAULT '80,100',
      expiry_alert_enabled   INTEGER NOT NULL DEFAULT 1,
      expiry_days            INTEGER NOT NULL DEFAULT 3,
      updated_at             TEXT    NOT NULL DEFAULT ''
    );

全局单行设置。`report_time` 是主机本地时区的 `HH:MM`。`traffic_thresholds` 是逗号分隔的百分比，在结构体里表现为 `[]int`，解析失败时回退 `[80, 100]`（宁可回到默认值也不要因脏数据让预警失效）。

    CREATE TABLE IF NOT EXISTS daily_usage (
      service_id INTEGER NOT NULL,
      day        TEXT    NOT NULL,
      in_bytes   INTEGER NOT NULL,
      out_bytes  INTEGER NOT NULL,
      fetched_at TEXT    NOT NULL,
      PRIMARY KEY (service_id, day)
    );

按 UTC+8 自然日缓存的日用量。`day` 是 `YYYY-MM-DD` 字符串，直接使用 BreaCloud 返回的 `bucket` 原值，不做时区换算后再格式化——任何二次转换都可能引入跨日漂移。

    CREATE TABLE IF NOT EXISTS traffic_alert_state (
      service_id INTEGER NOT NULL,
      threshold  INTEGER NOT NULL,
      alerted_at TEXT,
      PRIMARY KEY (service_id, threshold)
    );

流量预警的去重状态。`alerted_at` 非空表示该阈值已推送过、处于「已解除武装」状态。

    CREATE TABLE IF NOT EXISTS daily_job_runs (
      day    TEXT NOT NULL,
      job    TEXT NOT NULL,
      ran_at TEXT NOT NULL,
      PRIMARY KEY (day, job)
    );

每日任务的幂等标记。`day` 是主机本地日期，`job` 当前只有 `daily`。

## 为什么不用迁移框架

Schema 用单文件、全部 `CREATE TABLE IF NOT EXISTS`，启动时执行一次。项目处于起步期，改动 schema 直接删库重建即可，引入版本表与迁移目录是当前不需要的复杂度。

触发升级的信号很明确：出现「线上库不能丢」的实际需求时。那时再引入顺序迁移，并把这些 `IF NOT EXISTS` 作为基线。

## 连接与并发

- 驱动 `modernc.org/sqlite`（纯 Go，无 CGO，交叉编译方便），驱动名 `sqlite`。
- DSN 带 `_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)`。
- `SetMaxOpenConns(1)`：SQLite 只有单写者，连接池开大反而会拿到 `database is locked`。调度器与 Bot handler 的写操作靠这一层串行化，上层因此不需要加锁。

## 幂等播种

`Open()` 会执行一次 `INSERT OR IGNORE INTO settings (id, updated_at) VALUES (1, ?)`，保证单行设置总是存在，`Settings()` 不需要处理「查不到」的分支。

白名单同理：启动时把配置文件里的 `telegram.owner_chat_id` 用 `INSERT OR IGNORE` 播种进 `chats`。用户之后用 `/deny` 移除它，重启**不会**把它加回来——这正是 `INSERT OR IGNORE` 而不是「不存在就插入并删除多余项」的原因。

## 方法语义

- `UpdateSettings(ctx, fn)`：读—改—写，由调用方给出字段级修改函数，避免每个字段一个 update 语句。
- `NeedGreeting(ctx, chatID)`：判断 `greeted_at IS NULL`；`MarkGreeted` 写入时间。两个操作合起来保证招呼只发一次。
- `SaveDailyUsage(ctx, serviceID, buckets)`：按 `(service_id, day)` upsert。BreaCloud 的日桶会滚动修正（实测 15 分钟内数值翻倍后稳定），因此**后写覆盖先写**，不做累加。
- `SetAlerted(ctx, serviceID, threshold, at)`：`at` 为 nil 表示重新武装。预警状态机见 `docs/design/jobs.md`。
- `JobRan / MarkJobRan`：按 `(day, job)` 查询与写入，用于「今天是否已经跑过」。

## 相关文档

- 日桶的时间口径：`docs/design/README.md` 的「跨模块不变量」
- 预警状态机：`docs/design/jobs.md`
