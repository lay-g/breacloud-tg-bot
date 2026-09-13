# BreaCloud 平台接口

## `traffic-history` 的 `from` / `to` 参数无效

**现象**：按文档传入 `from=2020-01-01&to=2020-01-02`，返回的仍是最近 8 天数据；与不带这两个参数的响应 `cmp` 字节级相同。

**原因**：服务端未实现区间过滤，只认 `range` 枚举（`day` / `week` / `month`）；传 `1d` / `7d` / `30d` 一律 `ERR_INVALID_REQUEST`。

**解决**：只用 `range` 取数，不传 `from` / `to`。

**相关文件**：`internal/breacloud/client.go`、`docs/references/breacloud-api.md`

## `range=day` 的「昨天」桶被 24 小时窗口截断

**现象**：同一天的用量，`range=day` 比 `range=week` 少 37%（2026-09-12：22547300192 vs 30944777449 字节）。

**原因**：`range=day` 的 `daily` 是按「最近 24 小时窗口与该日的交集」聚合的，不是完整自然日；`range=week` 的日桶与 5 分钟样本按 UTC+8 求和逐字节相等，且与 `/traffic` 的周期总量相差 0.0003%。

**解决**：日用量一律取 `range=week` 的对应日桶。一次请求顺带覆盖 8 天，成本与 `range=day` 相同。

**相关文件**：`internal/jobs/report.go`、`docs/references/breacloud-api.md`

## 日桶时区与样本时间戳时区不一致

**现象**：把 `traffic-history` 的 5 分钟样本按 UTC 分组求和，与 `daily` 桶对不上；按 UTC+8 分组则逐字节相等。

**原因**：`daily[].bucket` 是后端本地日（UTC+8）的 `YYYY-MM-DD`；`points[].bucket` 是 UTC 时间戳且无时区后缀（形如 `2026-09-13 06:30`）。

**解决**：业务日期固定 `time.FixedZone("UTC+8", 8*3600)` 计算；日桶字符串原样使用，不做二次时区换算。

**相关文件**：`internal/jobs/report.go`、`internal/jobs/expiry.go`

## `quota_gb` / `used_gb` 的单位是 GiB

**现象**：`used_gb=429`，而 `in_bytes+out_bytes=459900591730`，按 10^9 换算得 459.9 GB，与 429 相差甚远。

**原因**：接口返回的是 GiB（2^30）。

**解决**：阈值比较按 `quota_gb << 30`，不要用 `1e9`。

**相关文件**：`internal/jobs/traffic.go`

## 接口在连续请求下偶发 TCP 层超时

**现象**：约 1 请求/秒的速率下出现 `curl: (28) Connection timed out after 30002 ms`；40 次突发里有 2 次同样失败。响应头没有任何限流标头，也没有 429。

**原因**：未公开的限流或连接数限制。

**解决**：客户端内置并发上限（默认 5）与退避重试（1s/2s/4s + 抖动）。只读 GET 可重试；`POST /services/:id/actions` 非幂等，不重试。

**相关文件**：`internal/breacloud/client.go`

## `/meta/locations` 存在重复主键

**现象**：`locations[]` 里 `id=7` 出现两次（`name` 都是 `lax-gia-main-10`），`pool_id` 分别是 3 和 18。

**原因**：同一节点被登记在两个 pool 下。

**解决**：不要依赖 `node_id → pool_id → 区域名` 做唯一映射。区域分组直接用服务列表里的 `region_name`（城市级）。

**相关文件**：`docs/references/breacloud-api.md`
