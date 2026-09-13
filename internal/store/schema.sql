-- 唯一的 schema 来源，启动时按顺序执行。
--
-- 全部使用 IF NOT EXISTS，可重复执行。项目处于起步期，schema 变更直接删库重建；
-- 等出现「线上库不能丢」的需求时再引入顺序迁移，并把当前内容作为基线。

-- 白名单。写入本表即表示授权该 chat 访问 Bot。
-- greeted_at 为 NULL 表示尚未发送首次招呼。
CREATE TABLE IF NOT EXISTS chats (
  chat_id    INTEGER PRIMARY KEY,
  greeted_at TEXT,
  created_at TEXT NOT NULL
);

-- 全局单行设置。可在 Bot 内调整的项都放这里，密钥留在配置文件。
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

-- 按 UTC+8 自然日缓存的日用量。day 直接使用 BreaCloud 返回的 bucket 原值。
CREATE TABLE IF NOT EXISTS daily_usage (
  service_id INTEGER NOT NULL,
  day        TEXT    NOT NULL,
  in_bytes   INTEGER NOT NULL,
  out_bytes  INTEGER NOT NULL,
  fetched_at TEXT    NOT NULL,
  PRIMARY KEY (service_id, day)
);

-- 流量预警去重状态。alerted_at 非空表示该阈值已推送过、处于「已解除武装」状态。
CREATE TABLE IF NOT EXISTS traffic_alert_state (
  service_id INTEGER NOT NULL,
  threshold  INTEGER NOT NULL,
  alerted_at TEXT,
  PRIMARY KEY (service_id, threshold)
);

-- 每日任务幂等标记。day 是主机本地日期。
CREATE TABLE IF NOT EXISTS daily_job_runs (
  day    TEXT NOT NULL,
  job    TEXT NOT NULL,
  ran_at TEXT NOT NULL,
  PRIMARY KEY (day, job)
);
