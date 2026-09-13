# BreaCloud API 接口参考

## 文档地址

- AI 可读手册（纯文本，本项目的权威来源）：<https://brea.cloud/api/v1/meta/llms.txt>
- OpenAPI 3 规范：<https://brea.cloud/api/v1/meta/openapi.json>
- 文档数据（客户+公开接口）：<https://brea.cloud/api/v1/meta/api-docs>
- 站点：<https://brea.cloud>

抓取时（2026-09-13）手册共 2075 行、132 个路径。手册与 OpenAPI 都不含 `components/schemas`，字段说明以手册正文为准。

## 认证

- 基址 `https://brea.cloud`，接口前缀 `/api/v1`。
- 请求头：`Authorization: Bearer <jwt 或 bll_ API Token>`。
- API Token 是**逐接口授权**的：每个接口标注 `scope`，Token 未授权时返回 HTTP 403 与 `{"code":"ERR_FORBIDDEN","message":"API Token 缺少权限: 需要 <scope>"}`。Token 无效或缺失返回 HTTP 401 与 `{"code":"ERR_UNAUTHORIZED","message":"未登录"}`。
- 参数非法返回 HTTP 400（例如 `status` 传了枚举外的值）。

## 响应信封

所有接口返回同一结构：

    {"code": "OK", "message": "ok", "data": {...}}

`code == "OK"` 表示成功，其余为错误码（`ERR_UNAUTHORIZED`、`ERR_FORBIDDEN`、`ERR_INVALID_REQUEST`、`ERR_NOT_FOUND`、`ERR_RANGE_TOO_WIDE` 等）。金额单位为微元（1e-6 主单位），时间字段为 ISO8601，多数为 UTC。

## 本项目使用的接口

### GET /services

- scope：`client.svc.read.list`
- 查询参数：`search`、`status`、`page`、`page_size`
- `status` 合法值（枚举外返回 400）：`active`、`pending`、`suspended`、`terminated`、`cancelled`
- 响应**没有 total 字段**，只能翻页到空页为止；`page_size` 传 1000 也被接受
- 列表已包含渲染所需的大部分字段：`domain_name`、`status`、`primary_ip`、`product_name`、`region_name`、`os_name`、`bandwidth_mbps`、`billing_cycle`、`next_due_date`、`expire_at`、`auto_renew_enabled`、`renew_canceled_at`、`billing_mode`
- `region_name` 是**城市级**（实例值 `Los Angeles`），与 `/meta/locations` 的 `locations[].location` 对齐，拿不到线路级信息

### GET /services/:id

- scope：`client.svc.read.get`
- 响应 `data` 分两部分：`service` 与 `resource`
- `resource` 提供 `vmid`、`node_id`、`node_name`、`node_cluster`、`node_location`、`node_country`、`primary_ip`、`ipv6`、`os_template`、`cpu_cores`、`memory_mb`、`disk_gb`
- 另有 `onboot`、`billing_mode`、`usage_grace_hours`

### GET /services/:id/traffic

- scope：`client.svc.read.traffic`
- 返回当前计费周期的 `period_start`、`period_end`、`mode`、`quota_gb`、`used_gb`、`over_gb`、`in_bytes`、`out_bytes`、`unlimited`、`overage_per_gb`、`p95_enabled`
- **`quota_gb` / `used_gb` 的单位是 GiB（2^30）**，不是 GB（10^9）。实测：`used_gb=429` 时 `in_bytes+out_bytes=459900591730`，除以 2^30 得 428.3

### GET /services/:id/traffic-history

- scope：`client.svc.read.traffic_history`
- 查询参数 `range`、`from`、`to`、`detail`
- **`range` 只接受 `day`、`week`、`month`**，`1d` / `7d` / `30d` / `3d` / `year` 均返回 `ERR_INVALID_REQUEST`
- **`from` / `to` 被服务端完全忽略**：带 2020 年窗口与不带窗口的响应字节级相同
- `range=week` 返回 8 个日桶，`range=month` 返回 14 个（受服务创建时间限制），`range=day` 只返回 2 个
- `daily[]` 元素为 `{bucket, in_bytes, out_bytes, in_mbps, out_mbps}`，`bucket` 是 **UTC+8 自然日**的 `YYYY-MM-DD`
- `detail=1` 时额外返回 `points[]`（5 分钟样本，约 920 条封顶≈3.2 天），其 `bucket` 是 **UTC** 时间戳且无时区后缀（形如 `2026-09-13 06:30`）——与日桶的时区口径不同，混用会算错

### POST /services/:id/actions

- scope：`client.svc.power.action`
- body：`{"action": "..."}`
- `action` 取值：`start`（开机）、`shutdown`（温和关机）、`stop`（冷关机）、`reboot`（重启）、`cold_reboot`（冷重启）
- 响应只有 `{"queued": true}`，**只是入队**，真实结果要查任务接口

### GET /services/:id/tasks

- scope：`client.svc.read.tasks`
- 返回 `tasks[]`，元素包含 `id`、`op`、`label`、`created_at`、`delivered_at`、`status`、`finished_at`

### POST /services/batch/actions（备用，当前未使用）

- scope：`client.svc.power.batch`
- body：`{"service_ids": [...], "action": "..."}`，越权 id 会被静默过滤，响应形如 `{"queued": 0, "total": 1}`

### GET /meta/locations（无需授权）

- 返回 `locations[]`（`id`、`name`、`location`、`country`、`online`、`pool_id`）与 `regions[]`（`pool_id`、`name`、`location`、`billing_mode`、`lines[]`）

## 实测到的平台侧行为

以下四条均已用真实 Token 复现，详细证据见过程性文档（未纳入版本库）中的平台问题证据包：

1. `traffic-history` 的 `from` / `to` 参数无效，只能靠 `range` 取数。
2. `range=day` 的「昨天」日桶是「最近 24 小时窗口与该日的交集」，**系统性少算**。实测 2026-09-12：`range=day` 为 22547300192 字节，而 `range=week` 与 5 分钟样本按 UTC+8 日求和均为 30944777449 字节，后两者逐字节相等。
3. `range=week` / `month` 的日桶与计费口径自洽：`month` 日桶求和 460583191425，对比 `/traffic` 的周期总量 460584422181，相对差 0.0003%。
4. `GET /meta/locations` 存在重复主键：`id=7` 出现两次（`lax-gia-main-10`，`pool_id` 分别为 3 和 18），因此 `node_id → pool_id → 区域名` 的映射不保证唯一。

另有两条稳定性观察：历史日桶存在延迟回填（一次性观测到 15 分钟内翻倍后稳定）；接口在连续约 1 请求/秒的速率下会偶发 TCP 层连接超时（非 429），响应头无任何限流标头。

## 本项目强制的使用约定

- 日用量一律取 `range=week` 的对应日桶，不使用 `range=day`。
- 所有日期按 UTC+8 自然日理解；代码中固定 `time.FixedZone("UTC+8", 8*3600)`。
- 流量配额比较按 GiB（`quota_gb << 30`）。
- 只读请求可重试；`POST /services/:id/actions` 非幂等，不自动重试。
- 客户端必须自带退避重试与并发上限。
