# 设计文档

本目录按模块记录设计：职责边界、对外接口、关键数据流、失败处理与取舍理由。

与 `docs/plans/` 的区别：plans 是实施过程记录（不进版本库、随时改写），这里记录的是**结论性设计**，随代码一起演进。接口参考见 `docs/references/`。

## 分层

    cli ──> config, systemd
     │
     └──> serve ─┬─> bot     Telegram 交互（实现 notify.Notifier）
                 ├─> jobs    定时任务与预警文案
                 └─> notify  Notifier 接口 + dry-run 实现
                        │
                        ├─> breacloud   HTTP 客户端
                        ├─> store       SQLite
                        └─> humanize    展示格式化

依赖单向向下。`bot` 与 `jobs` 都依赖 `breacloud` 与 `store`，但彼此不互相依赖——它们之间只有两条连接：

- `jobs` 通过 `notify.Notifier` 发送，`bot` 实现这个接口；
- `bot` 的 `/report` 通过注入的 `bot.ReportFunc` 调用 `jobs.BuildDailyReport`，由 `serve` 完成接线。

因此定时任务可以在没有 Telegram 的环境下完整测试与运行（`serve --dry-run`），而两个包之间没有 import 关系。

## 模块一览

| 模块 | 路径 | 职责 | 状态 |
| --- | --- | --- | --- |
| cli | `internal/cli` | 命令解析、参数校验、生命周期编排 | 已实现 |
| config | `internal/config` | 配置加载、默认值、路径解析、必填校验 | 已实现 |
| systemd | `internal/systemd` | 用户级单元文件渲染与 `systemctl --user` 调用 | 已实现 |
| （部署） | `Dockerfile` / `docker-compose.yml` | 容器镜像与 Compose 部署，见 `docs/rules/docker.md` | 已实现 |
| version | `internal/version` | 构建期注入的版本信息 | 已实现 |
| store | `internal/store` | SQLite schema、sqlc 查询、业务读写方法 | 已实现 |
| breacloud | `internal/breacloud` | BreaCloud API 客户端、并发与重试、服务列表缓存 | 已实现 |
| bot | `internal/bot` | Telegram 启动、菜单、访问控制、交互视图与回调 | 已实现 |
| jobs | `internal/jobs` | 调度器、日报、到期提醒、流量预警，以及这些推送的文案 | 已实现 |
| notify | `internal/notify` | 通知接口（`bot` 与 dry-run 各一份实现） | 已实现 |
| humanize | `internal/humanize` | 公用的展示格式化：字节、时间、开关状态 | 已实现 |
| md | `internal/md` | MarkdownV2 转义与实体构造 | 已实现 |

## 跨模块不变量

这些约定散落在多个模块，改动任何一处都要同时检查其它模块。

1. **时间口径**：业务日期一律是 BreaCloud 后端本地日 **UTC+8**，代码中用包级 `time.FixedZone("UTC+8", 8*3600)` 固定，不依赖主机时区。主机本地时区只用于“通知时间”这类调度判断。`traffic-history` 的 5 分钟样本时间戳是 UTC，与日桶不同源，不得混用。
2. **日用量取数**：只用 `range=week` 的日桶。`range=day` 的昨天桶被服务端 24 小时窗口截断，会系统性少算。
3. **流量单位**：`quota_gb` / `used_gb` 是 GiB，比较时按 `quota_gb << 30`。
4. **消息是 MarkdownV2，且必须转义**：所有动态文本过 `md.Escape`（等宽值用 `md.Code`），实体在一行内闭合。漏掉任一环，Telegram 会拒收**整条**消息——不是排版难看，是用户什么都收不到。发送侧有降级为纯文本的兜底，但那只是安全网。
5. **回调数据格式**：统一为竖线分隔的短字符串（如 `v|<serviceID>`、`ac|<serviceID>|cold_reboot`），单条不超过 64 字节。区域等可能超长的标识一律用排序后的下标引用。
6. **凭据只在配置文件或环境变量里**：Telegram token 与 BreaCloud token 只存在于 `~/.config/breacloud-tg-bot/config.yaml`（0600）或容器注入的环境变量（`.env`，0600），不进数据库、不写日志、不进版本库、不进容器镜像。数据库只存可随时重建的状态与设置。
7. **SQLite 单写者**：`SetMaxOpenConns(1)` + WAL，所有写操作串行化。
8. **重试边界**：只读 GET 可退避重试；`POST /services/:id/actions` 非幂等，不重试，失败直接把错误交给用户。
9. **输出分工**：日志一律写 stderr（slog），stdout 只留给命令自身的输出，这样 `serve --dry-run` 的输出可以直接管道给其它工具。
10. **版本号只有一个来源**：仓库根目录的 `VERSION` 文件（形如 `v0.1.0`）。二进制（`make build` 与容器镜像）里的版本都取自它，发布 tag 必须与它完全一致，CI 会校验。

## 两条主要数据流

**Telegram 指令**：`bot` 收到更新 → 校验 chat 在白名单内 → 命中的 handler 调用 `breacloud`（必要时读 `store` 缓存）→ `views` 渲染纯文本 + `keyboards` 构造按钮 → 回复或就地编辑消息。只有“确认执行电源操作”这一条路径会写远端状态。

**每日报告**：`jobs` 的调度器到点 → 取服务列表（走 `breacloud` 的 TTL 缓存）→ 并发拉每台机器的 `traffic-history?range=week` → 日桶写入 `store.daily_usage` → 取昨日桶聚合总量与前五 → 经 `notify` 广播给白名单内所有 chat → 写 `store.daily_job_runs` 幂等标记。取数部分失败不阻断发送，失败台数写进消息末尾。
