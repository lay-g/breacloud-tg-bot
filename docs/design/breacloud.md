# breacloud — API 客户端

> 状态：设计中（M3 实现）。接口字段与实测行为见 `docs/references/breacloud-api.md`。

## 职责

把 BreaCloud 的 HTTP 接口包装成类型安全的 Go 方法，并把三件容易做错的事收敛在一处：统一信封解码、并发与重试策略、分页。上层（`bot` / `jobs`）不应该自己拼 URL、判 `code`、写重试循环。

## 文件划分

- `types.go`：与 API 对齐的结构体，只建模用得到的字段。
- `errors.go`：`APIError`，携带 HTTP 状态码、业务错误码与消息。
- `client.go`：唯一的 HTTP 出口与各接口方法。
- `cache.go`：带 TTL 的服务列表缓存。

## 唯一的 HTTP 出口

    func (c *Client) do(ctx, method, path string, body, out any) error

所有方法都经由它，因此并发信号量、超时、信封解码、错误分类、重试这几件事只需要实现一次，也不会出现某个方法漏掉重试或漏掉解码的情况。

## 错误模型

    type APIError struct {
        Status  int    // HTTP 状态码
        Code    string // 信封里的业务错误码
        Message string // 可直接展示给用户的中文消息
    }

三种情况都归一成 `APIError`：HTTP 非 2xx、信封 `code != "OK"`、响应不是合法 JSON。最后一种会把原始响应的前 200 字节附在消息里，否则排查「返回了一页 HTML」这类问题只能靠猜。

`ERR_FORBIDDEN` 的消息形如「API Token 缺少权限: 需要 client.svc.read.traffic」，直接透传给用户比转成「权限不足」更有用——能立刻看出该去后台勾哪个 scope。

## 并发与重试

- 并发上限由 `breacloud.concurrency` 控制（默认 5），用带缓冲 channel 做信号量。
- 重试仅覆盖：网络错误、超时、HTTP 5xx。最多 3 次，间隔 1s / 2s / 4s 并加 0~500ms 抖动。
- HTTP 4xx 与业务错误码不重试：参数错了重试多少次都是错的。
- **`POST /services/:id/actions` 不参与自动重试**。它是非幂等的电源操作，重试可能对一台已经关机的机器再下一次指令。失败时把 `APIError` 交给用户，由用户决定是否重试。

实测该接口在连续约 1 请求/秒的速率下会偶发 TCP 层连接超时（不是 429），所以退避重试不是可选项。

## 分页

`ListServices` 从 `page=1&page_size=100` 开始循环，直到返回空数组。上限 50 页（5000 台），超出直接报错而不是继续翻——真到这个规模说明假设已经失效，静默截断比报错更危险。

响应没有 `total` 字段，因此无法提前知道页数，只能「翻到空页为止」。

## 类型约定

- 保留 API 的原始字符串字段（如 `next_due_date`、`expire_at`），在 `jobs` 里再解析。客户端不做业务判断，只做传输。
- 时间字段可能是 `null`，也可能是 RFC3339；用 `string` 接收、空串表示缺失，比 `time.Time` 更能容忍服务端的格式漂移。
- 金额与字节一律 `int64`。

## 缓存

    type ServiceCache struct{ ... }
    func (s *ServiceCache) List(ctx, force bool) ([]Service, error)

TTL 默认 5 分钟，`bot` 的 `/vps` 与 `jobs` 的调度共用同一个实例。动机是服务列表在一次日报里会被读多次（渲染、聚合、消息里查名字），而大账号下每次刷新都要翻很多页。

「刷新」按钮与手动查看某台 VPS 的用量走 `force=true` 绕过缓存。

## 相关文档

- 接口清单与实测行为：`docs/references/breacloud-api.md`
- 哪些取数口径是强制的：`docs/design/README.md` 的「跨模块不变量」
