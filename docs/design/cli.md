# cli — 命令行入口

## 职责

把命令行参数翻译成一次具体动作，并把动作之间的编排（加载配置、装配日志、准备通知器、等待信号）集中在一处。**不含业务逻辑**：所有业务行为都在 `bot` / `jobs` / `store` / `breacloud` 里。

## 命令树

    breacloud-tg-bot
    ├── serve      在前台运行服务
    │   ├── --dry-run   不发送 Telegram 消息，把通知打到 stdout
    │   └── --run-now   立即执行一次每日任务后退出
    ├── service
    │   ├── install [--bot-token T] [--api-token T] [--chat-id ID] [--force]
    │   ├── uninstall [--purge]
    │   └── start | stop | restart | status
    └── version

全局持久 flag：`--config`（配置文件路径）、`--log-level`（覆盖配置里的日志级别）。

`help` 由 cobra 自动提供。

## 退出码语义

- 参数错误、配置缺失、业务失败：打印 `错误: <信息>` 到 stderr，退出码 1。
- 子命令转发外部命令（`systemctl`）失败时：**沿用其退出码，且不额外打印**。子命令的诊断信息已经直接写到终端，再包一层「错误: exit status 3」只是噪声，而且会掩盖 `systemctl status` 对未运行服务返回 3 的语义。实现见 `Execute()` 里对 `*exec.ExitError` 的处理。

## 各处装配时机

`--log-level` 在 `PersistentPreRunE` 里生效，此时还没读配置文件。`serve` 才会加载配置并按 `log.level` 重新装配 logger——`version`、`service start/stop/restart/status` 都不需要配置文件存在。

## serve 的行为

1. 加载配置并 `Validate()`，缺 token 时给出「运行 service install」的提示。
2. 打印人类可读的启动摘要（版本、配置路径、API 地址、数据库路径、dry-run 状态），并用 slog 记一条启动日志。
3. `--run-now`：执行一次每日任务后退出（便于无 Telegram 环境下端到端验证）。否则阻塞在 `signal.NotifyContext`（SIGINT/SIGTERM）上，收到信号后优雅退出。

进程必须常驻而不是执行完即退，否则 systemd 单元里的 `Restart=always` 会变成 5 秒一次的重启循环。

## service install 的流程与幂等

1. 检查 `systemctl` 是否存在。
2. 解析 `os.Executable()` 并做软链接解析。**路径落在 `go-build` 临时目录或以系统临时目录开头时直接拒绝**：`go run` 产生的二进制会在构建缓存清理后消失，注册成服务就是死单元。提示用户先 `go build -o bin/<name> .`。
3. 读取已有配置作为默认值，再按 `--flag` → 配置文件 → 交互询问的顺序补齐 bot token、API token、首个 chat id。
4. 只做格式校验：bot token 必须含冒号，API token 必须以 `bll_` 开头。**远端探活不在这里做**——那需要 `internal/breacloud` 客户端，用裸 HTTP 复刻一遍响应信封解析会立刻被客户端取代。
5. 写配置文件（目录 0700、文件 0600），写单元文件（0644），`daemon-reload`，`enable --now`。
6. 检查 linger 并给出提示，但不自动提权（不执行 sudo）。

可重复执行：配置文件与单元文件都是覆盖写入，`--force` 控制是否允许覆盖已存在的单元文件，`daemon-reload` 与 `enable` 天然幂等。

## 与 systemd 模块的边界

`cli` 只负责「问什么、什么时候问、失败时说什么」，单元文件内容与 `systemctl` 调用全部在 `internal/systemd`。这样单元模板可以在测试里断言，不需要真的装一个服务。

## 相关文档

- 配置字段与优先级：`docs/design/config.md`
- 服务单元与 linger：`docs/design/systemd.md`
